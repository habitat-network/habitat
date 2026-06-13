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
	repo *managedRepo,
) error {
	var buffered []bufferedEvent
	if err := rb.db.WithContext(ctx).
		Where("space = ? AND did = ?", repo.Space, repo.DID).
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

		if err := rb.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			txBuf := rb.WithTx(tx)

			prevRev := syntax.TID("")
			if repo != nil {
				prevRev = repo.Rev
			}
			if !eventChains(prevRev, event.Since) {
				if mErr := txBuf.repos.MarkDesynced(ctx, repo.Space, repo.DID); mErr != nil {
					return mErr
				}
				return tx.Where("space = ? AND did = ?", repo.Space, repo.DID).
					Delete(&bufferedEvent{}).Error
			}
			if aErr := txBuf.applyEvent(event, repo); aErr != nil {
				return aErr
			}
			return tx.Delete(&bufferedEvent{}, entry.ID).Error
		}); err != nil {
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
		if err := rb.drainRepo(ctx, &repo); err != nil {
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

func (rb *resyncBuffer) clearOrg(ctx context.Context, orgDID syntax.DID) error {
	return rb.db.WithContext(ctx).
		Where("space LIKE ?", "ats://"+orgDID.String()+"/%").
		Delete(&bufferedEvent{}).Error
}

func (rb *resyncBuffer) handleSpaceEvent(
	ctx context.Context,
	org *managedOrg,
	event events.Event,
) error {
	repo, err := rb.repos.GetRepo(ctx, event.Space, event.Repo)
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
