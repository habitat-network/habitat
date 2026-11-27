package foreignrepo

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	maxPending   = 4096
	maxClaim     = 20
	maxAttempts  = 8
	deliveryAge  = 24 * time.Hour
	leaseTime    = 30 * time.Second
	firstBackoff = 15 * time.Second
)

var ErrOutboxFull = errors.New("foreign invite notification outbox is full")

type inviteDelivery struct {
	Key        repoKey    `gorm:"embedded"`
	Rev        syntax.TID `gorm:"not null"`
	Digest     []byte     `gorm:"not null"`
	ReadyAt    int64      `gorm:"not null;index"`
	LeaseUntil int64      `gorm:"not null"`
	ExpiresAt  int64      `gorm:"not null;index"`
	Attempts   int        `gorm:"not null"`
}

func (inviteDelivery) TableName() string { return "foreign_invite_deliveries" }

// lockAndReap serializes bounded admission and claiming. On SQLite the first
// write reserves the database writer even when nothing has expired. Callers
// that also take repo locks always take them before this queue lock.
func lockAndReap(tx *gorm.DB, now int64) error {
	if tx.Name() == "postgres" {
		if err := tx.Exec(
			`SELECT pg_advisory_xact_lock(hashtextextended('habitat:foreign-invite-outbox', 0))`,
		).Error; err != nil {
			return err
		}
	}
	return tx.Where("expires_at <= ? OR (attempts >= ? AND lease_until <= ?)", now, maxAttempts, now).
		Delete(&inviteDelivery{}).
		Error
}

// enqueue is only called under the authored repo's write transaction, with
// its new head. That head persists after delivery, so an acknowledged queue
// row cannot be recreated by an older revision.
func enqueue(tx *gorm.DB, k scope, commit spacecommit.Commit, now time.Time) error {
	at := now.UnixMilli()
	if err := lockAndReap(tx, at); err != nil {
		return err
	}
	row := inviteDelivery{
		Key: k.key(), Rev: syntax.TID(commit.Rev), Digest: commit.Data,
		ReadyAt: at, ExpiresAt: now.Add(deliveryAge).UnixMilli(),
	}
	// Replacements need no new capacity and clear any old lease/backoff.
	result := k.match(tx.Model(&inviteDelivery{})).Where("rev < ?", row.Rev).
		Updates(map[string]any{
			"rev": row.Rev, "digest": row.Digest, "ready_at": row.ReadyAt,
			"expires_at": row.ExpiresAt, "attempts": 0, "lease_until": 0,
		})
	if result.Error != nil || result.RowsAffected != 0 {
		return result.Error
	}
	var count int64
	if err := k.match(tx.Model(&inviteDelivery{})).Count(&count).Error; err != nil {
		return err
	}
	if count != 0 {
		// Never replace a newer or equal pending event.
		return nil
	}
	if err := tx.Model(&inviteDelivery{}).Count(&count).Error; err != nil {
		return err
	}
	if count >= maxPending {
		return ErrOutboxFull
	}
	return tx.Create(&row).Error
}

// Notification is exactly the data a future notifyRepoUpdate sender will
// need, with no endpoint, token, or invite contents in the queue.
type Notification struct {
	Space  habitat_syntax.SpaceURI
	Author syntax.DID
	Rev    syntax.TID
	Digest [sha256.Size]byte
}

// Delivery is one leased notification. Keep it until Complete returns.
// Reading a lease or completing it does not make any network request.
type Delivery struct {
	notice     Notification
	attempt    int
	leaseUntil int64
	expiresAt  int64
}

// Notification returns a copy of the data bound to this lease.
func (d Delivery) Notification() Notification { return d.notice }

// ClaimDue leases at most 20 notifications. The attempt is charged before
// delivery so a repeatedly crashing worker cannot retry indefinitely.
func (s *Store) ClaimDue(ctx context.Context, now time.Time, limit int) ([]Delivery, error) {
	at := now.UnixMilli()
	limit = max(0, min(limit, maxClaim))
	if limit == 0 {
		return nil, nil
	}
	var items []Delivery
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAndReap(tx, at); err != nil {
			return err
		}
		var rows []inviteDelivery
		if err := tx.Where("ready_at <= ? AND lease_until <= ? AND attempts < ?", at, at, maxAttempts).
			Order("ready_at, space_owner, space_type, space_skey, author").
			Limit(limit).
			Find(&rows).
			Error; err != nil {
			return err
		}
		for _, row := range rows {
			inbox := habitat_syntax.ConstructSpaceURI(
				row.Key.SpaceOwner,
				row.Key.SpaceType,
				row.Key.SpaceSkey,
			)
			k, err := parseScope(inbox, row.Key.Author)
			if err != nil {
				return err
			}
			if _, err := syntax.ParseTID(
				row.Rev.String(),
			); err != nil ||
				len(row.Digest) != sha256.Size {
				return spacecommit.ErrInvalidCommit
			}
			lease := min(now.Add(leaseTime).UnixMilli(), row.ExpiresAt)
			res := k.match(tx.Model(&inviteDelivery{})).
				Where("rev = ? AND attempts = ? AND lease_until = ?", row.Rev, row.Attempts, row.LeaseUntil).
				Updates(map[string]any{"attempts": row.Attempts + 1, "lease_until": lease})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 1 {
				items = append(items, Delivery{
					notice: Notification{
						Space: inbox.AtSpace(), Author: row.Key.Author,
						Rev: row.Rev, Digest: [sha256.Size]byte(row.Digest),
					},
					attempt: row.Attempts + 1, leaseUntil: lease, expiresAt: row.ExpiresAt,
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("claim foreign invite deliveries: %w", err)
	}
	return items, nil
}

// Outcome is a future worker's classification of a delivery attempt.
type Outcome int

const (
	Delivered Outcome = iota + 1
	Retry
	PermanentFailure
)

// Complete applies the outcome to exactly this still-current, unexpired
// lease. A superseding revision or expired lease turns it into a no-op.
// Retriable failures back off; permanent failures are not kept or retried.
func (s *Store) Complete(
	ctx context.Context, item Delivery, now time.Time, outcome Outcome,
) (bool, error) {
	k, err := parseScope(item.notice.Space, item.notice.Author)
	if err != nil {
		return false, err
	}
	if item.attempt < 1 || item.attempt > maxAttempts {
		return false, errors.New("invalid notification lease")
	}
	at := now.UnixMilli()
	q := k.match(s.db.WithContext(ctx).Model(&inviteDelivery{})).
		Where("rev = ? AND attempts = ? AND lease_until = ? AND lease_until > ? AND expires_at > ?",
			item.notice.Rev, item.attempt, item.leaseUntil, at, at)
	var result *gorm.DB
	switch outcome {
	case Delivered, PermanentFailure:
		result = q.Delete(&inviteDelivery{})
	case Retry:
		next := now.Add(firstBackoff << (item.attempt - 1)).UnixMilli()
		if item.attempt >= maxAttempts || next >= item.expiresAt {
			result = q.Delete(&inviteDelivery{})
		} else {
			result = q.Updates(map[string]any{"ready_at": next, "lease_until": 0})
		}
	default:
		return false, errors.New("invalid notification outcome")
	}
	if result.Error != nil {
		return false, fmt.Errorf("complete foreign invite delivery: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}
