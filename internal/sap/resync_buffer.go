package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

type resyncBuffer struct {
	db    *gorm.DB
	repos *repoManager
}

var _ db.Store[*resyncBuffer] = (*resyncBuffer)(nil)

func newResyncBuffer(db *gorm.DB, repos *repoManager) *resyncBuffer {
	return &resyncBuffer{db: db, repos: repos}
}

func (rb *resyncBuffer) WithTx(tx *gorm.DB) *resyncBuffer {
	return &resyncBuffer{
		db:    tx,
		repos: rb.repos,
	}
}

func (rb *resyncBuffer) shouldBuffer(org *managedOrg, repo *managedRepo) bool {
	if org.CrawlState != nil && *org.CrawlState == crawlStateRunning {
		return true
	}
	if repo == nil {
		return true
	}
	switch repo.State {
	case RepoStatePending, RepoStateResyncing, RepoStateDesynced:
		return true
	default:
		return false
	}
}

func (rb *resyncBuffer) appendEvent(event events.Event) error {
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return rb.db.Create(&bufferedEvent{
		Space: event.Space,
		DID:   event.Repo,
		Seq:   event.Seq,
		Data:  data,
	}).Error
}

func eventChains(prevRev syntax.TID, since syntax.TID) bool {
	if since == "" {
		return true
	}
	if prevRev == "" {
		return false
	}
	return prevRev == since
}

func (rb *resyncBuffer) getRepo(
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) (*managedRepo, error) {
	var repo managedRepo
	err := rb.db.Where("space = ? AND did = ?", space, did).First(&repo).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &repo, nil
}

func (rb *resyncBuffer) applyEvent(event events.Event, repo *managedRepo) error {
	var prevRev syntax.TID
	if repo != nil {
		prevRev = repo.Rev
	}

	if !eventChains(prevRev, event.Since) {
		return rb.db.Model(&managedRepo{}).
			Where("space = ? AND did = ?", event.Space, event.Repo).
			Update("state", RepoStateDesynced).Error
	}

	if err := rb.db.Save(&managedRepo{
		Space: event.Space,
		DID:   event.Repo,
		Rev:   event.Rev,
		State: RepoStateActive,
	}).Error; err != nil {
		return err
	}
	return writeEventOps(rb.db, event.Ops)
}

func (rb *resyncBuffer) drainRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	var buffered []bufferedEvent
	if err := rb.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		Order("seq ASC").
		Find(&buffered).Error; err != nil {
		return fmt.Errorf("load buffered events: %w", err)
	}
	if len(buffered) == 0 {
		return nil
	}

	for _, entry := range buffered {
		var event events.Event
		if err := json.Unmarshal(entry.Data, &event); err != nil {
			return fmt.Errorf("unmarshal buffered event: %w", err)
		}

		repo, err := rb.repos.GetRepo(ctx, space, did)
		if err != nil {
			return err
		}
		prevRev := syntax.TID("")
		if repo != nil {
			prevRev = repo.Rev
		}
		if !eventChains(prevRev, event.Since) {
			if err := rb.repos.MarkDesynced(ctx, space, did); err != nil {
				return err
			}
			return rb.db.WithContext(ctx).
				Where("space = ? AND did = ?", space, did).
				Delete(&bufferedEvent{}).Error
		}

		err = rb.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := rb.WithTx(tx).applyEvent(event, repo); err != nil {
				return err
			}
			return tx.Delete(&bufferedEvent{}, entry.ID).Error
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (rb *resyncBuffer) drainOrg(ctx context.Context, orgDID syntax.DID) error {
	var repos []managedRepo
	if err := rb.db.WithContext(ctx).
		Where("space LIKE ? AND state = ?", "ats://"+orgDID.String()+"/%", RepoStateActive).
		Find(&repos).Error; err != nil {
		return fmt.Errorf("load active repos: %w", err)
	}

	var errs []error
	for _, repo := range repos {
		if err := rb.drainRepo(ctx, repo.Space, repo.DID); err != nil {
			errs = append(errs, err)
			slog.ErrorContext(
				ctx,
				"drain repo buffer",
				"space", repo.Space,
				"repo", repo.DID,
				"err", err,
			)
		}
	}
	return errors.Join(errs...)
}

func (rb *resyncBuffer) handleSpaceEvent(org *managedOrg, event events.Event) error {
	repo, err := rb.getRepo(event.Space, event.Repo)
	if err != nil {
		return err
	}
	if repo == nil {
		repo = &managedRepo{
			Space: event.Space,
			DID:   event.Repo,
			State: RepoStatePending,
		}
		if err := rb.db.Create(repo).Error; err != nil {
			return err
		}
	}

	if rb.shouldBuffer(org, repo) {
		return rb.appendEvent(event)
	}

	return rb.applyEvent(event, repo)
}
