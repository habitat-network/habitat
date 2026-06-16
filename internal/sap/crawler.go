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

type crawler struct {
	db         *gorm.DB
	orgManager *orgManager
}

func newCrawler(
	db *gorm.DB,
	orgManager *orgManager,
) *crawler {
	return &crawler{
		db:         db,
		orgManager: orgManager,
	}
}

func (c *crawler) resumeIncompleteCrawls(ctx context.Context) error {
	var orgs []managedOrg
	if err := c.db.WithContext(ctx).
		Where("access_token != ''").
		Where("crawl_state = ?", crawlStateRunning).
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
		// TODO: cancel subscriptions
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
				slog.ErrorContext(ctx, "enumerate space members", "space", space.Uri, "err", err)
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
	return nil
}
