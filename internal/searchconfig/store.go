// Package searchconfig stores how each org's collections are searched, as
// network.habitat.search.config records in the org's opensocial members
// space, keyed by collection NSID. A collection with a record is surfaced in
// search results for the spaces the org owns; [DefaultCollections] are
// surfaced for every org without one.
package searchconfig

import (
	"cmp"
	"context"
	"encoding/json"
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
}

// Config is how one collection is searched.
type Config struct {
	// Collection is the NSID of the records this configures, and the key of
	// its record.
	Collection syntax.NSID
	// CrawlableFields are the paths of the record fields whose text is
	// indexed. Empty means every text field.
	CrawlableFields []string
	// FilterableFields are the paths of the record fields a search may
	// filter on. Empty means none.
	FilterableFields []string
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

// Put writes cfg for org, creating it or replacing the existing config for
// the collection.
func (s *Store) Put(ctx context.Context, org syntax.DID, cfg Config) error {
	recordBytes, err := spaces.MarshalRecord(habitat_api.NetworkHabitatSearchConfig{
		CrawlableFields:  cfg.CrawlableFields,
		FilterableFields: cfg.FilterableFields,
		UpdatedAt:        time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal search config record: %w", err)
	}
	if _, _, err := s.spaces.PutRecord(
		ctx, space(org), org, Collection, syntax.RecordKey(cfg.Collection), recordBytes,
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

// List returns the configs org's admins wrote, sorted by collection. It
// excludes [DefaultCollections].
func (s *Store) List(ctx context.Context, org syntax.DID) ([]Config, error) {
	collection := syntax.NSID(Collection)
	records, err := s.spaces.ListRecords(ctx, space(org), org, &collection)
	if err != nil {
		return nil, fmt.Errorf("list search config records: %w", err)
	}
	configs := make([]Config, 0, len(records))
	for _, record := range records {
		var r habitat_api.NetworkHabitatSearchConfig
		raw, err := json.Marshal(record.Value)
		if err != nil {
			return nil, fmt.Errorf("marshal search config record: %w", err)
		}
		if err := json.Unmarshal(raw, &r); err != nil {
			return nil, fmt.Errorf("decode search config record: %w", err)
		}
		nsid, err := syntax.ParseNSID(string(record.Rkey))
		if err != nil {
			// Not written by Put, so not a collection config.
			continue
		}
		configs = append(configs, Config{
			Collection:       nsid,
			CrawlableFields:  r.CrawlableFields,
			FilterableFields: r.FilterableFields,
		})
	}
	slices.SortFunc(configs, func(a, b Config) int {
		return cmp.Compare(a.Collection, b.Collection)
	})
	return configs, nil
}

// Collections returns the collections each of orgs configured. An org with
// none is absent from the result. It is how [search.Searcher] learns what to
// surface.
func (s *Store) Collections(
	ctx context.Context,
	orgs ...syntax.DID,
) (map[syntax.DID][]syntax.NSID, error) {
	out := make(map[syntax.DID][]syntax.NSID)
	for _, org := range orgs {
		configs, err := s.List(ctx, org)
		if err != nil {
			return nil, err
		}
		for _, cfg := range configs {
			out[org] = append(out[org], cfg.Collection)
		}
	}
	return out, nil
}
