package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/db"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

var _ db.Store[*crawler] = (*crawler)(nil)

type crawler struct {
	db         *gorm.DB
	orgManager *orgManager
	repos      *repoManager
	resyncBuf  *resyncBuffer
	sub        *subscriber
}

func (c *crawler) WithTx(tx *gorm.DB) *crawler {
	return &crawler{
		db:         tx,
		orgManager: c.orgManager,
		repos:      c.repos,
		resyncBuf:  c.resyncBuf,
		sub:        c.sub,
	}
}

func newCrawler(
	db *gorm.DB,
	orgManager *orgManager,
	repos *repoManager,
	resyncBuf *resyncBuffer,
	sub *subscriber,
) *crawler {
	return &crawler{
		db:         db,
		orgManager: orgManager,
		repos:      repos,
		resyncBuf:  resyncBuf,
		sub:        sub,
	}
}

func (c *crawler) resumeIncompleteCrawls(ctx context.Context) {
	var orgs []managedOrg
	err := c.db.WithContext(ctx).
		Where("access_token != ''").
		Where("crawl_state IS NULL OR crawl_state = ?", crawlStateRunning).
		Find(&orgs).Error
	if err != nil {
		slog.ErrorContext(ctx, "load orgs for crawl", "err", err)
		return
	}
	for i := range orgs {
		go c.crawlOrg(ctx, &orgs[i])
	}
	<-ctx.Done()
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
	}

	if err := c.resyncBuf.drainOrg(ctx, org.DID); err != nil {
		slog.ErrorContext(ctx, "drain org buffer", "org", org.DID, "err", err)
	}
	slog.InfoContext(ctx, "crawler finished", "org", org.DID)
}

func (c *crawler) resumeCrawl(ctx context.Context, org *managedOrg) error {
	client := c.orgManager.GetClient(ctx, org.DID)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var cursor crawlCursor
		err := c.db.WithContext(ctx).First(&cursor, "org = ?", org.DID).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("find cursor: %w", err)
		}

		params := url.Values{}
		if cursor.Cursor != "" {
			params.Set("cursor", cursor.Cursor)
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
				slog.ErrorContext(ctx, "enumerate space members", "space", space.Uri, "err", err)
			}
		}

		if listSpacesOutput.Cursor == "" {
			break
		}

		if err := c.db.WithContext(ctx).Save(&crawlCursor{
			Org:    org.DID,
			Cursor: listSpacesOutput.Cursor,
		}).Error; err != nil {
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
		if err := c.repos.EnsureRepo(ctx, space, syntax.DID(member.Did)); err != nil {
			return err
		}
	}
	return nil
}
