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

// resyncBuffer processes events from the subscriber before sending to the outbox.
// if the resyncer is in progress, then events are persisted in the buffer to be drained
// at the end of the resync.
type resyncBuffer struct {
	db            *gorm.DB
	resyncNotifCh chan struct{}
}

var _ db.Store[*resyncBuffer] = (*resyncBuffer)(nil)

func newResyncBuffer(db *gorm.DB, resyncNotifCh chan struct{}) *resyncBuffer {
	return &resyncBuffer{db: db, resyncNotifCh: resyncNotifCh}
}

func (rb *resyncBuffer) WithTx(tx *gorm.DB) *resyncBuffer {
	return &resyncBuffer{
		db:            tx,
		resyncNotifCh: rb.resyncNotifCh,
	}
}

func (rb *resyncBuffer) shouldBuffer(org *managedOrg, repo *managedRepo, event events.Event) bool {
	if org.CrawlState != nil && *org.CrawlState == crawlStateRunning {
		return true
	}
	if repo == nil && event.Since != "" {
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
	if !eventChains(repo.Rev, event.Since) {
		if event.Since > repo.Rev {
			err := rb.db.Model(&managedRepo{}).
				Where("space = ? AND did = ?", event.Space, event.Repo).
				Update("state", RepoStateDesynced).Error
			if err == nil {
				rb.notify()
			}
			return err
		}
		// past event that we've already seen
		return nil
	}
	if err := rb.db.Model(&managedRepo{}).
		Where("space = ? AND did = ?", event.Space, event.Repo).
		Updates(map[string]any{"rev": event.Rev, "state": RepoStateActive}).
		Error; err != nil {
		return err
	}
	return writeEventOps(rb.db, event.Ops)
}

func (rb *resyncBuffer) drainRepo(
	ctx context.Context,
	repo *managedRepo,
) error {
	// Single-pass drain. If a live event is buffered during the transition
	// window (subscriber loaded stale state before SetActive committed), the
	// subscriber's next event for this repo will fail the chain check and mark
	// the repo desynced. The resyncer picks up desynced repos, so no events
	// are lost — they arrive in the full resync.
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

	rev := repo.Rev
	for _, entry := range buffered {
		var event events.Event
		if err := json.Unmarshal(entry.Data, &event); err != nil {
			return fmt.Errorf("unmarshal buffered event: %w", err)
		}

		if err := rb.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if !eventChains(rev, event.Since) {
				if event.Since > rev {
					if mErr := tx.Model(&managedRepo{}).
						Where("space = ? AND did = ?", repo.Space, repo.DID).
						Update("state", RepoStateDesynced).Error; mErr != nil {
						return mErr
					}
					slog.WarnContext(ctx, "missed events", "space", repo.Space, "did", repo.DID)
					rb.notify()
				}
				return tx.Where("space = ? AND did = ?", repo.Space, repo.DID).
					Delete(&bufferedEvent{}).Error
			}

			if err := tx.Model(&managedRepo{}).
				Where("space = ? AND did = ?", repo.Space, repo.DID).
				Updates(map[string]any{"rev": event.Rev, "state": RepoStateActive}).
				Error; err != nil {
				return err
			}
			rev = event.Rev
			if err := writeEventOps(tx, event.Ops); err != nil {
				return err
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
	var repo managedRepo
	err := rb.db.WithContext(ctx).
		Where("space = ? AND did = ?", event.Space, event.Repo).
		First(&repo).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		repo = managedRepo{
			Space: event.Space,
			DID:   event.Repo,
			State: RepoStatePending,
		}
		if err := rb.db.Create(&repo).Error; err != nil {
			return err
		}
		rb.notify()
	}

	if rb.shouldBuffer(org, &repo, event) {
		return rb.appendEvent(event)
	}

	return rb.applyEvent(event, &repo)
}

func (rb *resyncBuffer) notify() {
	select {
	case rb.resyncNotifCh <- struct{}{}:
	default:
	}
}
