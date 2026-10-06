// Package searchconfig stores how each org's collections are searched, as
// network.habitat.search.config records in the org's opensocial members
// space, keyed by collection NSID. A collection with a record is surfaced in
// search results for the spaces the org owns; [DefaultCollections] are
// surfaced for every org without one.
package searchconfig

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_api "github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Collection is the collection search config records are written to.
const Collection = "network.habitat.search.config"

// DefaultCollections are included in search results for every org, whether or
// not its admins configure them. They are a code-level list so a new
// deployment searches something useful before any admin configures anything.
var DefaultCollections = []syntax.NSID{
	"network.habitat.docs.markdown",
	"network.habitat.docs.comment",
	"network.habitat.docs.commentReply",
}

// Store reads and writes search config records in the spaces store.
type Store struct {
	spaces spaces.Store
}

// NewStore returns a Store over spacesStore.
func NewStore(spacesStore spaces.Store) *Store {
	return &Store{spaces: spacesStore}
}

// Defaults returns the collections included for every org.
func (s *Store) Defaults() []syntax.NSID {
	return slices.Clone(DefaultCollections)
}

func space(org syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
}

// Add surfaces collection in org's search results by writing its config
// record. Adding one that is already configured does nothing.
func (s *Store) Add(ctx context.Context, org syntax.DID, collection syntax.NSID) error {
	recordBytes, err := spaces.MarshalRecord(habitat_api.NetworkHabitatSearchConfig{
		UpdatedAt: time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal search config record: %w", err)
	}
	if _, _, err := s.spaces.PutRecord(
		ctx, space(org), org, Collection, syntax.RecordKey(collection), recordBytes,
	); err != nil {
		return fmt.Errorf("put search config record: %w", err)
	}
	return nil
}

// Remove deletes the config for collection from org. Removing one that isn't
// configured does nothing.
func (s *Store) Remove(ctx context.Context, org syntax.DID, collection syntax.NSID) error {
	_, err := s.spaces.GetRecord(
		ctx, space(org), org, Collection, syntax.RecordKey(collection),
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return fmt.Errorf("get search config record: %w", err)
	}
	if err := s.spaces.DeleteRecord(
		ctx, space(org), org, Collection, collection.String(),
	); err != nil {
		return fmt.Errorf("delete search config record: %w", err)
	}
	return nil
}

// List returns the collections org's admins configured, sorted. It excludes
// [DefaultCollections].
func (s *Store) List(ctx context.Context, org syntax.DID) ([]syntax.NSID, error) {
	collection := syntax.NSID(Collection)
	records, err := s.spaces.ListRecords(ctx, space(org), org, &collection)
	if err != nil {
		return nil, fmt.Errorf("list search config records: %w", err)
	}
	collections := make([]syntax.NSID, 0, len(records))
	for _, record := range records {
		nsid, err := syntax.ParseNSID(string(record.Rkey))
		if err != nil {
			// Not written by Add, so not a collection config.
			continue
		}
		collections = append(collections, nsid)
	}
	slices.Sort(collections)
	return collections, nil
}

// Collections is [Store.List], for [search.Searcher].
func (s *Store) Collections(ctx context.Context, org syntax.DID) ([]syntax.NSID, error) {
	return s.List(ctx, org)
}
