// Package notify persists syncer registrations (network.habitat.space.registerNotify)
// and delivers notifyWrite events to the registered endpoints when a repo in a
// space advances, per the permissioned-data sync proposal. notifySpaceDeleted
// deliveries are persisted and retried until they succeed or the syncer's
// registration expires.
package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// registration is the GORM model for a persisted notify registration. A
// registration keyed on an empty Repo subscribes to writes from every repo in
// the space; a registration with a Repo subscribes to that repo only.
//
// Audience, not Endpoint, identifies a registration: it is the service
// identifier the subscriber registered under, and it is what the delivery's
// service-auth JWT is addressed to. Endpoint is only where the call goes, and
// is a plain column so that two services resolving to the same address stay
// distinct registrations. Registrations predating the service field carry their
// endpoint URL as the audience; see the audience migration.
type registration struct {
	Space    habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Repo     syntax.DID              `gorm:"primaryKey"`
	Audience string                  `gorm:"primaryKey"`
	Endpoint string
	// Registrations predating the column were all made through
	// network.habitat.space.registerNotify.
	Namespace Namespace `gorm:"default:network.habitat.space"`
	ExpiresAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Namespace is the lexicon namespace a registration was made through. It
// decides which methods the registration's notifications are delivered as, and
// how their service-auth JWT is addressed (see Registration.audience).
type Namespace string

const (
	// NamespaceHabitat is network.habitat.space.registerNotify.
	NamespaceHabitat Namespace = "network.habitat.space"
	// NamespaceAtproto is com.atproto.space.registerNotify.
	NamespaceAtproto Namespace = "com.atproto.space"
)

// method names the notification method name (e.g. "notifyWrite") in ns.
func (ns Namespace) method(name string) syntax.NSID {
	return syntax.NSID(string(ns) + "." + name)
}

// Registration is the public view of a persisted notify registration.
type Registration struct {
	Space habitat_syntax.SpaceURI
	Repo  syntax.DID // empty subscribes to the whole space
	// Audience is the service identifier deliveries are addressed to.
	Audience string
	// Endpoint is the resolved delivery address notifyWrite is sent to.
	Endpoint string
	// Namespace is the lexicon namespace the registration was made through.
	Namespace Namespace
	// ExpiresAt is when the registration lapses unless renewed.
	ExpiresAt time.Time
}

// Store persists syncer registrations.
type Store interface {
	// Register upserts a registration for (space, repo, audience), refreshing
	// its expiry to expiresAt and recording endpoint as the address to deliver
	// to and ns as the namespace it was made through. An empty repo registers for the whole space; an audience equal to
	// the endpoint URL marks a registration made through the deprecated
	// endpoint field.
	Register(
		ctx context.Context,
		ns Namespace,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		audience string,
		endpoint string,
		expiresAt time.Time,
	) error
	// ListForRepo returns the unexpired registrations that should receive a
	// notifyWrite for a write to repo within space: both whole-space
	// registrations and registrations targeting that specific repo.
	ListForRepo(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
	) ([]Registration, error)
	// ListForSpace returns every unexpired registration for the space,
	// regardless of repo. Used to fan out notifySpaceDeleted.
	ListForSpace(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) ([]Registration, error)

	// QueueSpaceDeleted persists a pending notifySpaceDeleted delivery to each
	// of regs, one per audience, due at retryAt (unix milliseconds). A
	// delivery already pending for the same (space, audience) restarts from
	// its first attempt, so a space deleted again re-notifies its syncers.
	QueueSpaceDeleted(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		regs []Registration,
		retryAt int64,
	) ([]PendingDeletion, error)
	// DueSpaceDeleted returns up to limit pending deliveries due at or before
	// now (unix milliseconds), earliest first.
	DueSpaceDeleted(ctx context.Context, now int64, limit int) ([]PendingDeletion, error)
	// RescheduleSpaceDeleted moves p to retryAt with its attempt count set to
	// attempts, but only if p is still due at p.RetryAt: a caller that loses
	// the race to another worker, or to a later deletion re-queueing p, gets
	// false and leaves the row alone. Claiming a due delivery is a reschedule
	// to a lease that keeps the attempt count.
	RescheduleSpaceDeleted(
		ctx context.Context,
		p PendingDeletion,
		attempts int,
		retryAt int64,
	) (bool, error)
	// ClearSpaceDeleted drops p once it is delivered or abandoned, under the
	// same p.RetryAt guard as RescheduleSpaceDeleted.
	ClearSpaceDeleted(ctx context.Context, p PendingDeletion) error
}

// spaceDeletedDelivery is the GORM model for a notifySpaceDeleted delivery
// that hasn't succeeded yet. It copies the registration it is delivered to,
// since registrations outlive neither their expiry nor, in future, the space.
//
// RetryAt is unix milliseconds rather than a timestamp so it compares exactly
// in the guarded updates on every dialect.
type spaceDeletedDelivery struct {
	Space     habitat_syntax.SpaceURI `gorm:"primaryKey"`
	Audience  string                  `gorm:"primaryKey"`
	Endpoint  string
	Namespace Namespace
	// ExpiresAt is the registration's expiry: past it the syncer must renew
	// its credential, which answers SpaceDeleted, so delivery stops.
	ExpiresAt time.Time
	Attempts  int
	RetryAt   int64 `gorm:"index"`
}

// PendingDeletion is the public view of a pending notifySpaceDeleted delivery.
type PendingDeletion struct {
	Registration
	// Attempts counts the failed deliveries so far.
	Attempts int
	// RetryAt is when the delivery is next due, in unix milliseconds.
	RetryAt int64
}

type store struct {
	db *gorm.DB
}

var _ Store = &store{}

func NewStore(db *gorm.DB) (*store, error) {
	return &store{db: db}, nil
}

func (s *store) Register(
	ctx context.Context,
	ns Namespace,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	audience string,
	endpoint string,
	expiresAt time.Time,
) error {
	// Upsert on the (space, repo, audience) key so re-registering refreshes the
	// expiry rather than accumulating duplicates, and so a subscriber whose
	// service now resolves to a different address updates in place.
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "space"}, {Name: "repo"}, {Name: "audience"},
		},
		DoUpdates: clause.AssignmentColumns(
			[]string{"endpoint", "namespace", "expires_at", "updated_at"},
		),
	}).Create(&registration{
		Space:     space,
		Repo:      repo,
		Audience:  audience,
		Endpoint:  strings.TrimRight(endpoint, "/"),
		Namespace: ns,
		ExpiresAt: expiresAt,
	}).Error
}

func (s *store) ListForRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) ([]Registration, error) {
	return s.list(s.db.WithContext(ctx).
		Where("space = ?", space).
		Where("repo = ? OR repo = ?", repo, ""))
}

func (s *store) ListForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) ([]Registration, error) {
	return s.list(s.db.WithContext(ctx).Where("space = ?", space))
}

// list runs query with the shared unexpired filter and maps rows to the public
// Registration view.
func (s *store) list(query *gorm.DB) ([]Registration, error) {
	var rows []registration
	if err := query.Where("expires_at > ?", time.Now()).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list registrations: %w", err)
	}

	regs := make([]Registration, len(rows))
	for i, row := range rows {
		regs[i] = Registration{
			Space:     row.Space,
			Repo:      row.Repo,
			Audience:  row.Audience,
			Endpoint:  row.Endpoint,
			Namespace: row.Namespace,
			ExpiresAt: row.ExpiresAt,
		}
	}
	return regs, nil
}

func (s *store) QueueSpaceDeleted(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	regs []Registration,
	retryAt int64,
) ([]PendingDeletion, error) {
	// A syncer registered for several repos in the space is one audience and
	// gets one notification; keep the registration that lasts longest.
	byAudience := make(map[string]Registration, len(regs))
	var order []string
	for _, reg := range regs {
		prev, ok := byAudience[reg.Audience]
		if !ok {
			order = append(order, reg.Audience)
		}
		if !ok || reg.ExpiresAt.After(prev.ExpiresAt) {
			byAudience[reg.Audience] = reg
		}
	}
	if len(order) == 0 {
		return nil, nil
	}

	rows := make([]spaceDeletedDelivery, len(order))
	pending := make([]PendingDeletion, len(order))
	for i, audience := range order {
		reg := byAudience[audience]
		reg.Space = space
		reg.Repo = ""
		rows[i] = spaceDeletedDelivery{
			Space:     space,
			Audience:  audience,
			Endpoint:  reg.Endpoint,
			Namespace: reg.Namespace,
			ExpiresAt: reg.ExpiresAt,
			RetryAt:   retryAt,
		}
		pending[i] = PendingDeletion{Registration: reg, RetryAt: retryAt}
	}
	err := s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "space"}, {Name: "audience"}},
		DoUpdates: clause.AssignmentColumns(
			[]string{"endpoint", "namespace", "expires_at", "attempts", "retry_at"},
		),
	}).Create(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("queue space deleted deliveries: %w", err)
	}
	return pending, nil
}

func (s *store) DueSpaceDeleted(
	ctx context.Context,
	now int64,
	limit int,
) ([]PendingDeletion, error) {
	var rows []spaceDeletedDelivery
	err := s.db.WithContext(ctx).
		Where("retry_at <= ?", now).
		Order("retry_at ASC").
		Limit(limit).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list due space deleted deliveries: %w", err)
	}
	pending := make([]PendingDeletion, len(rows))
	for i, row := range rows {
		pending[i] = PendingDeletion{
			Registration: Registration{
				Space:     row.Space,
				Audience:  row.Audience,
				Endpoint:  row.Endpoint,
				Namespace: row.Namespace,
				ExpiresAt: row.ExpiresAt,
			},
			Attempts: row.Attempts,
			RetryAt:  row.RetryAt,
		}
	}
	return pending, nil
}

func (s *store) RescheduleSpaceDeleted(
	ctx context.Context,
	p PendingDeletion,
	attempts int,
	retryAt int64,
) (bool, error) {
	res := s.pendingDeletion(ctx, p).Updates(map[string]any{
		"attempts": attempts,
		"retry_at": retryAt,
	})
	if res.Error != nil {
		return false, fmt.Errorf("reschedule space deleted delivery: %w", res.Error)
	}
	return res.RowsAffected == 1, nil
}

func (s *store) ClearSpaceDeleted(ctx context.Context, p PendingDeletion) error {
	if err := s.pendingDeletion(ctx, p).Delete(&spaceDeletedDelivery{}).Error; err != nil {
		return fmt.Errorf("clear space deleted delivery: %w", err)
	}
	return nil
}

// pendingDeletion scopes a query to p's row, as long as it is still due at the
// time p was read: matching retry_at leaves alone a row another worker claimed
// or a later deletion re-queued.
func (s *store) pendingDeletion(ctx context.Context, p PendingDeletion) *gorm.DB {
	return s.db.WithContext(ctx).
		Model(&spaceDeletedDelivery{}).
		Where("space = ? AND audience = ? AND retry_at = ?", p.Space, p.Audience, p.RetryAt)
}

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&registration{}, &spaceDeletedDelivery{}}
}
