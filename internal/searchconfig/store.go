// Package searchconfig stores how each org's collections are indexed and
// searched, as network.habitat.search.config records in the org's opensocial
// members space, keyed by collection NSID. A collection with a record is
// indexed and searched in the spaces the org owns, as the record says;
// [DefaultCollections] are for every org, and a record for one overrides
// its default config.
package searchconfig

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_api "github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Collection is the collection search config records are written to.
const Collection = search.ConfigCollection

// DefaultCollections are included in search results for every org, whether or
// not its admins configure them. They are a code-level list so a new
// deployment searches something useful before any admin configures anything.
var DefaultCollections = []syntax.NSID{
	"network.habitat.docs.markdown",
	"network.habitat.docs.comment",
	"network.habitat.docs.commentReply",
	"network.habitat.drop.file",
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

// Add indexes and searches collection for org by writing its config record,
// which configures nothing so the collection gets the default config. Adding
// one that is already configured does nothing, and keeps its config.
func (s *Store) Add(ctx context.Context, org syntax.DID, collection syntax.NSID) error {
	_, err := s.spaces.GetRecord(
		ctx, space(org), org, Collection, syntax.RecordKey(collection),
	)
	if err == nil {
		return nil
	} else if !errors.Is(err, spaces.ErrRecordNotFound) {
		return fmt.Errorf("get search config record: %w", err)
	}
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
// [DefaultCollections] unless a record overrides one.
func (s *Store) List(ctx context.Context, org syntax.DID) ([]syntax.NSID, error) {
	records, err := s.records(ctx, org)
	if err != nil {
		return nil, err
	}
	return slices.Sorted(maps.Keys(records)), nil
}

// Configs implements [search.ConfigSource]: a [search.DefaultConfig] for each
// of [DefaultCollections], and the config each of org's records gives. A
// record that can't be parsed gets the default config, and is logged.
func (s *Store) Configs(
	ctx context.Context,
	org syntax.DID,
) (map[syntax.NSID]search.CollectionConfig, error) {
	records, err := s.records(ctx, org)
	if err != nil {
		return nil, err
	}
	configs := make(map[syntax.NSID]search.CollectionConfig, len(DefaultCollections)+len(records))
	for _, c := range DefaultCollections {
		configs[c] = search.DefaultConfig()
	}
	for c, record := range records {
		cfg, err := search.ParseConfig(record.Value, record.Rev)
		if err != nil {
			slog.WarnContext(ctx, "search: using the default config for an invalid one",
				"org", org, "collection", c, "err", err)
			cfg = search.DefaultConfig()
			// Still a new version, so a fix to it is picked up.
			cfg.Rev = record.Rev
		}
		configs[c] = cfg
	}
	return configs, nil
}

// records returns org's config records by collection.
func (s *Store) records(
	ctx context.Context,
	org syntax.DID,
) (map[syntax.NSID]spaces.Record, error) {
	collection := Collection
	records, err := s.spaces.ListRecords(ctx, space(org), org, &collection)
	if err != nil {
		return nil, fmt.Errorf("list search config records: %w", err)
	}
	out := make(map[syntax.NSID]spaces.Record, len(records))
	for _, record := range records {
		nsid, err := syntax.ParseNSID(string(record.Rkey))
		if err != nil {
			// Not keyed by a collection, so not a collection config.
			continue
		}
		out[nsid] = record
	}
	return out, nil
}
