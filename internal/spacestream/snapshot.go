package spacestream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"

	"github.com/habitat-network/habitat/internal/spaces"
)

// Bounds on a snapshot page, from the network.habitat.space.getSnapshot
// lexicon.
const (
	DefaultSnapshotLimit = 100
	MaxSnapshotLimit     = 1000
)

// SnapshotQuery is a getSnapshot request's parameters.
type SnapshotQuery struct {
	// After is exclusive: the snapshot starts at the seq after it.
	After spaces.EventSeq
	// Before is inclusive; nil takes the log's current head.
	Before *spaces.EventSeq
	Limit  int
	Filter spaces.EventFilter
}

// ParseSnapshotQuery parses the network.habitat.space.getSnapshot query
// parameters. The spaces, dids and collections filters are those of subscribe.
func ParseSnapshotQuery(q url.Values) (SnapshotQuery, error) {
	// cursor is subscribe's own parameter, so the filter parsing is shared.
	filterOnly := url.Values{}
	for _, k := range []string{"spaces", "dids", "collections"} {
		filterOnly[k] = q[k]
	}
	base, err := ParseQuery(filterOnly)
	if err != nil {
		return SnapshotQuery{}, err
	}
	out := SnapshotQuery{Filter: base.Filter, Limit: DefaultSnapshotLimit}
	seq := func(name string) (*spaces.EventSeq, error) {
		raw := q.Get(name)
		if raw == "" {
			return nil, nil
		}
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%w: %s must be a non-negative integer", ErrInvalidQuery, name)
		}
		return new(spaces.EventSeq(n)), nil
	}
	after, err := seq("afterSeq")
	if err != nil {
		return SnapshotQuery{}, err
	}
	if after != nil {
		out.After = *after
	}
	if out.Before, err = seq("beforeSeq"); err != nil {
		return SnapshotQuery{}, err
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > MaxSnapshotLimit {
			return SnapshotQuery{}, fmt.Errorf("%w: limit must be 1 to %d",
				ErrInvalidQuery, MaxSnapshotLimit)
		}
		out.Limit = n
	}
	return out, nil
}

// SnapshotPage is one page of a snapshot.
type SnapshotPage struct {
	// Events are the events the reader may see, as the JSON of the
	// subscribe #commit and #spaceDeleted objects, in seq order.
	Events []json.RawMessage
	// PlannedThroughSeq is the last seq the page covered, event or not.
	PlannedThroughSeq spaces.EventSeq
	// TipSeq is where the snapshot ends.
	TipSeq spaces.EventSeq
}

// Snapshot reads one page of the log up to a pinned tip, authorizing events
// as a stream would. A page ends when it holds query.Limit events or reaches
// the tip, so a reader that sees few of the log's events still makes progress
// without a long scan: a page also ends after scanning maxScan log entries.
// sub's Query is ignored in favor of query. A space credential's filter is
// narrowed to its space, and a spaces filter that excludes it is
// [ErrCredentialMismatch].
func (s *Server) Snapshot(
	ctx context.Context,
	sub Subscription,
	query SnapshotQuery,
) (SnapshotPage, error) {
	head, err := s.source.SequenceEvents(ctx)
	if err != nil {
		return SnapshotPage{}, fmt.Errorf("sequence events: %w", err)
	}
	tip := head
	if query.Before != nil && *query.Before < tip {
		tip = *query.Before
	}
	sub.Query = Query{Filter: query.Filter}
	if sub, err = Prepare(sub); err != nil {
		return SnapshotPage{}, err
	}

	page := SnapshotPage{Events: []json.RawMessage{}, PlannedThroughSeq: query.After, TipSeq: tip}
	if query.After > tip {
		page.PlannedThroughSeq = tip
		return page, nil
	}
	next := query.After + 1
	access := newAccessCache(s.roles, sub)
	scanned := 0
	for next <= tip {
		events, err := s.source.ListEvents(ctx, sub.Query.Filter, next, s.pageSize)
		if err != nil {
			return SnapshotPage{}, fmt.Errorf("list events: %w", err)
		}
		for _, event := range events {
			if event.EventSeq() > tip {
				page.PlannedThroughSeq = tip
				return page, nil
			}
			ok, err := access.allows(ctx, event)
			if err != nil {
				return SnapshotPage{}, fmt.Errorf("authorize event: %w", err)
			}
			if ok {
				frame, err := eventPayload(event)
				if err != nil {
					return SnapshotPage{}, err
				}
				page.Events = append(page.Events, frame)
			}
			page.PlannedThroughSeq = event.EventSeq()
			next = event.EventSeq() + 1
			scanned++
			if len(page.Events) >= query.Limit {
				return page, nil
			}
		}
		if len(events) < s.pageSize {
			// The filtered log has nothing more before the tip.
			page.PlannedThroughSeq = tip
			return page, nil
		}
		if scanned >= maxSnapshotScan {
			return page, nil
		}
	}
	page.PlannedThroughSeq = tip
	return page, nil
}

// maxSnapshotScan bounds how many log entries one page reads, so a reader the
// log mostly isn't for gets a (possibly empty) page and a cursor to continue
// from, rather than a request that scans the whole log.
const maxSnapshotScan = 10_000
