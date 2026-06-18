package sap

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// crawler lists spaces and their members to discover repos once an org is added
type crawler struct {
	db            *gorm.DB
	orgManager    *orgManager
	resyncBuf     *resyncBuffer
	sub           *subscriber
	resyncNotifCh chan struct{}
}

func newCrawler(
	db *gorm.DB,
	orgManager *orgManager,
	resyncBuf *resyncBuffer,
	sub *subscriber,
	resyncNotifCh chan struct{},
) *crawler {
	return &crawler{
		db:            db,
		orgManager:    orgManager,
		resyncBuf:     resyncBuf,
		sub:           sub,
		resyncNotifCh: resyncNotifCh,
	}
}

func (c *crawler) resumeIncompleteCrawls(ctx context.Context) error {
	var orgs []managedOrg
	if err := c.db.WithContext(ctx).
		Where("access_token != ''").
		Where(c.db.Where("crawl_state = ?", crawlStateRunning).Or("crawl_state IS NULL")).
		Find(&orgs).Error; err != nil {
		return fmt.Errorf("find incomplete crawls: %w", err)
	}
	for i := range orgs {
		go c.crawlOrg(ctx, &orgs[i])
	}
	return nil
}

func (c *crawler) crawlOrg(ctx context.Context, org *managedOrg) {
	if err := c.db.WithContext(ctx).
		Model(&managedOrg{}).
		Where("did = ?", org.DID).
		Update("crawl_state", crawlStateRunning).Error; err != nil {
		slog.ErrorContext(ctx, "set crawl running", "org", org.DID, "err", err)
		return
	}

	crawlErr := c.resumeCrawl(ctx, org)

	if crawlErr != nil {
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
		return
	}

	if err := c.resyncBuf.drainOrg(ctx, org.DID); err != nil {
		slog.ErrorContext(ctx, "drain org buffer", "org", org.DID, "err", err)
	}
	slog.InfoContext(ctx, "crawler finished", "org", org.DID)
}

func (c *crawler) resumeCrawl(ctx context.Context, org *managedOrg) error {
	client := c.orgManager.GetClient(ctx, org.DID)
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

		resp, err := client.Get(
			org.Host + "/xrpc/network.habitat.space.listSpaces?" + params.Encode(),
		)
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
			if err := c.enumerateSpaceMembers(ctx, client, org, space.Uri); err != nil {
				return fmt.Errorf("enumerate space members for %s: %w", space.Uri, err)
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

func (c *crawler) enumerateSpaceMembers(
	ctx context.Context,
	client *http.Client,
	org *managedOrg,
	spaceURI string,
) error {
	values := url.Values{"space": []string{spaceURI}}
	resp, err := client.Get(
		org.Host + "/xrpc/network.habitat.space.getMembers?" + values.Encode(),
	)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var getMembersOutput habitat.NetworkHabitatSpaceGetMembersOutput
	if err := json.NewDecoder(resp.Body).Decode(&getMembersOutput); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("get members: %s", resp.Status)
	}

	space := habitat_syntax.SpaceURI(spaceURI)
	for _, member := range getMembersOutput.Members {
		if err := c.db.WithContext(ctx).
			Clauses(clause.OnConflict{DoNothing: true}).
			Create(&managedRepo{
				Space: space,
				DID:   syntax.DID(member.Did),
				State: RepoStatePending,
			}).Error; err != nil {
			return err
		}
	}
	select {
	case c.resyncNotifCh <- struct{}{}:
	default:
	}
	return nil
}
