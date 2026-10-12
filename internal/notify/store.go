// Package notify persists syncer registrations (network.habitat.space.registerNotify)
// and delivers notifyWrite events to the registered endpoints when a repo in a
// space advances, per the permissioned-data sync proposal.
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

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&registration{}}
}
