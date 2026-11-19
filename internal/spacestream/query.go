package spacestream

import (
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Caps on the filters, from the lexicon (and Jetstream's, for dids and
// collections). They bound the memory and query size one subscriber can cost.
const (
	MaxSpaces      = 100
	MaxDIDs        = 10_000
	MaxCollections = 100
)

// ErrInvalidQuery is returned by [ParseQuery] for a malformed or over-cap
// parameter.
var ErrInvalidQuery = errors.New("invalid subscribe query")

// Query is a subscribe request's parameters.
type Query struct {
	// Cursor is where to resume, inclusive; nil starts at the live tip.
	Cursor *spaces.EventSeq
	Filter spaces.EventFilter
}

// ParseQuery parses the network.habitat.space.subscribe query parameters.
// Array parameters repeat (spaces=a&spaces=b) and are deduplicated before
// their caps apply.
func ParseQuery(q url.Values) (Query, error) {
	var query Query
	if raw := q.Get("cursor"); raw != "" {
		cursor, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || cursor < 0 {
			return Query{}, fmt.Errorf("%w: cursor must be a non-negative integer", ErrInvalidQuery)
		}
		query.Cursor = new(spaces.EventSeq(cursor))
	}

	var err error
	query.Filter.Spaces, err = parseList(q["spaces"], MaxSpaces, "spaces",
		habitat_syntax.ParseSpaceURI)
	if err != nil {
		return Query{}, err
	}
	query.Filter.Repos, err = parseList(q["dids"], MaxDIDs, "dids", syntax.ParseDID)
	if err != nil {
		return Query{}, err
	}

	// A collection is an NSID, or "<prefix>.*" for every collection under
	// prefix. Like Jetstream, the prefix itself isn't validated as an NSID,
	// so "app.bsky.*" works.
	patterns, err := parseList(q["collections"], MaxCollections, "collections",
		func(raw string) (string, error) {
			if prefix, ok := strings.CutSuffix(raw, "*"); ok {
				if !strings.HasSuffix(prefix, ".") || len(prefix) < 2 ||
					strings.Contains(prefix, "*") {
					return "", fmt.Errorf("invalid collection pattern %q", raw)
				}
				return raw, nil
			}
			nsid, err := syntax.ParseNSID(raw)
			return nsid.String(), err
		})
	if err != nil {
		return Query{}, err
	}
	for _, p := range patterns {
		if prefix, ok := strings.CutSuffix(p, "*"); ok {
			query.Filter.CollectionPrefixes = append(query.Filter.CollectionPrefixes, prefix)
		} else {
			query.Filter.Collections = append(query.Filter.Collections, syntax.NSID(p))
		}
	}
	return query, nil
}

// parseList parses and deduplicates the values of one array parameter.
func parseList[T comparable](
	values []string,
	maxLen int,
	name string,
	parse func(string) (T, error),
) ([]T, error) {
	if len(values) == 0 {
		return nil, nil
	}
	seen := make(map[T]bool, len(values))
	parsed := make([]T, 0, len(values))
	for _, raw := range values {
		v, err := parse(raw)
		if err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrInvalidQuery, name, err)
		}
		if seen[v] {
			continue
		}
		seen[v] = true
		parsed = append(parsed, v)
		if len(parsed) > maxLen {
			return nil, fmt.Errorf("%w: at most %d %s", ErrInvalidQuery, maxLen, name)
		}
	}
	return parsed, nil
}
