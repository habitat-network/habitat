package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type crawler struct {
	db          *gorm.DB
	oauthClient *oauthclient.App
	resyncBuf   *resyncBuffer
	sub         *subscriber
	resyncNotif *utils.PollNotifier
	metrics     *metrics
}

func newCrawler(
	db *gorm.DB,
	oauthClient *oauthclient.App,
	resyncBuf *resyncBuffer,
	sub *subscriber,
	resyncNotif *utils.PollNotifier,
	metrics *metrics,
) *crawler {
	return &crawler{
		db:          db,
		oauthClient: oauthClient,
		resyncBuf:   resyncBuf,
		sub:         sub,
		resyncNotif: resyncNotif,
		metrics:     metrics,
	}
}

func (c *crawler) resumeIncompleteCrawls(ctx context.Context) error {
	ctx, span := c.metrics.tracer.Start(ctx, "sap.crawler.resume_incomplete_crawls")
	defer span.End()

	var orgs []managedOrg
	if err := c.db.WithContext(ctx).
		Where("crawl_state = ?", crawlStateRunning).
		Or("crawl_state IS NULL").
		Find(&orgs).Error; err != nil {
		span.RecordError(err)
		return fmt.Errorf("find incomplete crawls: %w", err)
	}
	span.SetAttributes(attribute.Int("sap.crawls_resumed", len(orgs)))
	for i := range orgs {
		go c.crawlOrg(inheritCancelDetachSpan(ctx), &orgs[i])
	}
	return nil
}

func (c *crawler) crawlOrg(ctx context.Context, org *managedOrg) {
	ctx, span := c.metrics.tracer.Start(ctx, "sap.crawler.crawl_org",
		trace.WithAttributes(attribute.String("sap.org", org.DID.String())))
	start := time.Now()
	c.metrics.crawlStarted(ctx)
	status := "error"
	defer func() {
		c.metrics.crawlFinished(ctx, start, status)
		span.End()
	}()

	if err := c.db.WithContext(ctx).
		Model(&managedOrg{}).
		Where("did = ?", org.DID).
		Update("crawl_state", crawlStateRunning).Error; err != nil {
		slog.ErrorContext(ctx, "set crawl running", "org", org.DID, "err", err)
		span.RecordError(err)
		return
	}

	crawlErr := c.resumeCrawl(ctx, org)

	if crawlErr != nil {
		span.RecordError(crawlErr)
		if err := c.db.WithContext(ctx).
			Model(&managedOrg{}).
			Where("did = ?", org.DID).
			Updates(map[string]any{
				"crawl_state": crawlStateErrored,
				"error_msg":   crawlErr.Error(),
			}).Error; err != nil {
			slog.ErrorContext(ctx, "set crawl errored", "org", org.DID, "err", err)
		}

		if err := c.resyncBuf.clearOrg(ctx, org.DID); err != nil {
			slog.ErrorContext(ctx, "clear org buffer", "org", org.DID, "err", err)
		}

		c.sub.cancelSubscription(org.DID)
		slog.ErrorContext(ctx, "crawl failed", "org", org.DID, "err", crawlErr)
		return
	}

	if err := c.db.WithContext(ctx).
		Model(&managedOrg{}).
		Where("did = ?", org.DID).
		Update("crawl_state", crawlStateComplete).Error; err != nil {
		slog.ErrorContext(ctx, "set crawl complete", "org", org.DID, "err", err)
		span.RecordError(err)
		return
	}
	status = "success"

	if err := c.resyncBuf.drainOrg(ctx, org.DID); err != nil {
		slog.ErrorContext(ctx, "drain org buffer", "org", org.DID, "err", err)
		span.RecordError(err)
	}
	slog.InfoContext(ctx, "crawler finished", "org", org.DID)
}

func (c *crawler) resumeCrawl(ctx context.Context, org *managedOrg) error {
	client, err := c.oauthClient.GetClient(ctx, org.DID, org.SessionID)
	if err != nil {
		return fmt.Errorf("resume session: %w", err)
	}

	cursor := org.CrawlCursor
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{}
		if cursor != "" {
			params.Set("cursor", cursor)
		}

		resp, err := client.Get("/xrpc/network.habitat.space.listSpaces?" + params.Encode())
		if err != nil {
			return fmt.Errorf("list spaces: %w", err)
		}

		var listSpacesOutput habitat.NetworkHabitatSpaceListSpacesOutput
		decodeErr := json.NewDecoder(resp.Body).Decode(&listSpacesOutput)
		closeErr := resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode list spaces output: %w", decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("list spaces: %s", resp.Status)
		}

		if len(listSpacesOutput.Spaces) == 0 {
			break
		}

		for _, space := range listSpacesOutput.Spaces {
			if err := c.enumerateSpaceRepos(ctx, client, space.Uri); err != nil {
				return fmt.Errorf("enumerate space repos for %s: %w", space.Uri, err)
			}
		}

		if listSpacesOutput.Cursor == "" {
			break
		}
		cursor = listSpacesOutput.Cursor

		if err := c.db.WithContext(ctx).
			Model(&managedOrg{}).
			Where("did = ?", org.DID).
			Update("crawl_cursor", listSpacesOutput.Cursor).Error; err != nil {
			return fmt.Errorf("save crawl cursor: %w", err)
		}
	}
	return nil
}

func (c *crawler) enumerateSpaceRepos(
	ctx context.Context,
	client *http.Client,
	spaceURI string,
) error {
	space := habitat_syntax.SpaceURI(spaceURI)

	// A space shared between managed DIDs (an org and its members) shows up in
	// every one of their listSpaces. Skip it if another DID's crawl already
	// enumerated it — its repos, including everyone's, were recorded then.
	var claim crawledSpace
	err := c.db.WithContext(ctx).Where("space = ?", space).First(&claim).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("check crawled space: %w", err)
	}

	values := url.Values{"space": []string{spaceURI}}
	resp, err := client.Get("/xrpc/network.habitat.space.listRepos?" + values.Encode())
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var listReposOutput habitat.NetworkHabitatSpaceListReposOutput
	if err := json.NewDecoder(resp.Body).Decode(&listReposOutput); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list repos: %s", resp.Status)
	}

	// Record the repos and the crawled-space claim atomically, so an interrupted
	// crawl leaves the space unclaimed and gets re-enumerated on resume rather
	// than skipped with repos missing.
	if err := c.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, repo := range listReposOutput.Repos {
			if err := tx.Clauses(clause.OnConflict{DoNothing: true}).
				Create(&managedRepo{
					Space: space,
					DID:   syntax.DID(repo.Did),
					State: RepoStatePending,
				}).Error; err != nil {
				return err
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).
			Create(&crawledSpace{Space: space}).Error
	}); err != nil {
		return err
	}
	slog.InfoContext(
		ctx,
		"enumerate space repos, notifying resync",
		"space",
		space,
		"repos",
		len(listReposOutput.Repos),
	)
	c.resyncNotif.Notify()
	return nil
}
