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
	"github.com/habitat-network/habitat/internal/utils"
	"gorm.io/gorm"
)

// resyncBuffer processes events from the subscriber before sending to the outbox.
// if the resyncer is in progress, then events are persisted in the buffer to be drained
// at the end of the resync.
type resyncBuffer struct {
	db          *gorm.DB
	resyncNotif *utils.PollNotifier
	outboxNotif *utils.PollNotifier
}

var _ db.Store[*resyncBuffer] = (*resyncBuffer)(nil)

func newResyncBuffer(db *gorm.DB, resyncNotif, outboxNotif *utils.PollNotifier) *resyncBuffer {
	return &resyncBuffer{db: db, resyncNotif: resyncNotif, outboxNotif: outboxNotif}
}

func (rb *resyncBuffer) WithTx(tx *gorm.DB) *resyncBuffer {
	return &resyncBuffer{
		db:          tx,
		resyncNotif: rb.resyncNotif,
		outboxNotif: rb.outboxNotif,
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
		return prevRev == ""
	}
	if prevRev == "" {
		return false
	}
	return prevRev == since
}

func (rb *resyncBuffer) applyEvent(event events.Event, repo *managedRepo) error {
	fmt.Printf("[sap-debug] applyEvent space=%s repo=%s rev=%s since=%s numOps=%d\n", event.Space, event.Repo, event.Rev, event.Since, len(event.Ops))
	for i, op := range event.Ops {
		fmt.Printf("[sap-debug] applyEvent op[%d] action=%s uri=%s\n", i, op.Action, op.Uri)
	}
	if !eventChains(repo.Rev, event.Since) {
		if event.Since > repo.Rev {
			fmt.Printf("[sap-debug] applyEvent desync space=%s repo=%s rev=%s since=%s\n", event.Space, event.Repo, repo.Rev, event.Since)
			err := rb.db.Model(&managedRepo{}).
				Where("space = ? AND did = ?", event.Space, event.Repo).
				Update("state", RepoStateDesynced).Error
			if err == nil {
				rb.resyncNotif.Notify()
			}
			return err
		}
		fmt.Printf("[sap-debug] applyEvent past event, skipping space=%s repo=%s\n", event.Space, event.Repo)
		return nil
	}
	fmt.Printf("[sap-debug] applyEvent updating repo state to active\n")
	if err := rb.db.Model(&managedRepo{}).
		Where("space = ? AND did = ?", event.Space, event.Repo).
		Updates(map[string]any{"rev": event.Rev, "state": RepoStateActive}).
		Error; err != nil {
		return err
	}
	if err := writeEventOps(rb.db, event.Ops); err != nil {
		fmt.Printf("[sap-debug] applyEvent writeEventOps error: %v\n", err)
		return err
	}
	rb.outboxNotif.Notify()
	fmt.Printf("[sap-debug] applyEvent done, ops written to outbox\n")
	return nil
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
					rb.resyncNotif.Notify()
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
		rb.outboxNotif.Notify()
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
	fmt.Printf("[sap-debug] handleSpaceEvent space=%s repo=%s seq=%d numOps=%d type=%s\n", event.Space, event.Repo, event.Seq, len(event.Ops), event.Type)
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
		rb.resyncNotif.Notify()
		fmt.Printf("[sap-debug] created pending repo space=%s did=%s\n", event.Space, event.Repo)
	}

	if rb.shouldBuffer(org, &repo, event) {
		fmt.Printf("[sap-debug] buffering event space=%s repo=%s state=%s\n", event.Space, event.Repo, repo.State)
		return rb.appendEvent(event)
	}

	fmt.Printf("[sap-debug] applying event space=%s repo=%s state=%s\n", event.Space, event.Repo, repo.State)
	return rb.applyEvent(event, &repo)
}
