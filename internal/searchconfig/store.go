// Package searchconfig stores which record collections an org's admins
// surface in search results. [github.com/habitat-network/habitat/internal/search]
// narrows every query to those collections plus [DefaultCollections].
package searchconfig

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// DefaultCollections are included in search results for every org, whether or
// not its admins configure them. They are a code-level list so a new
// deployment searches something useful before any admin configures anything.
var DefaultCollections = []syntax.NSID{
	"network.habitat.docs.markdown",
	"network.habitat.docs.comment",
}

// collection is one collection an org surfaces in search.
type collection struct {
	OrgDID     syntax.DID  `gorm:"column:org_did;primaryKey"`
	Collection syntax.NSID `gorm:"primaryKey"`
	CreatedAt  time.Time
}

func (collection) TableName() string { return "search_collections" }

// Store persists the collections each org surfaces in search.
type Store struct {
	db *gorm.DB
}

// NewStore returns a Store over db.
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// Models returns the GORM models this package persists. Their tables are
// created by db.Migrate.
func Models() []any {
	return []any{&collection{}}
}

// Defaults returns the collections included for every org.
func (s *Store) Defaults() []syntax.NSID {
	return slices.Clone(DefaultCollections)
}

// Add surfaces c in org's search results. Adding a collection already
// configured does nothing.
func (s *Store) Add(ctx context.Context, org syntax.DID, c syntax.NSID) error {
	err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&collection{OrgDID: org, Collection: c}).Error
	if err != nil {
		return fmt.Errorf("add search collection: %w", err)
	}
	return nil
}

// Remove stops surfacing c in org's search results. Removing a collection
// that isn't configured does nothing.
func (s *Store) Remove(ctx context.Context, org syntax.DID, c syntax.NSID) error {
	err := s.db.WithContext(ctx).
		Where("org_did = ? AND collection = ?", org, c).
		Delete(&collection{}).Error
	if err != nil {
		return fmt.Errorf("remove search collection: %w", err)
	}
	return nil
}

// List returns the collections org's admins configured, sorted. It excludes
// [DefaultCollections].
func (s *Store) List(ctx context.Context, org syntax.DID) ([]syntax.NSID, error) {
	var rows []collection
	err := s.db.WithContext(ctx).
		Where("org_did = ?", org).
		Order("collection").
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list search collections: %w", err)
	}
	out := make([]syntax.NSID, len(rows))
	for i, r := range rows {
		out[i] = r.Collection
	}
	return out, nil
}

// ListAll returns the collections every org configured. An org with none is
// absent from the result.
func (s *Store) ListAll(ctx context.Context) (map[syntax.DID][]syntax.NSID, error) {
	var rows []collection
	err := s.db.WithContext(ctx).Order("org_did, collection").Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("list all search collections: %w", err)
	}
	out := make(map[syntax.DID][]syntax.NSID)
	for _, r := range rows {
		out[r.OrgDID] = append(out[r.OrgDID], r.Collection)
	}
	return out, nil
}
