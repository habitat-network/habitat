// Package spacestream serves network.habitat.space.subscribe: one sequence of
// the record writes and space deletions this host holds, across every space a
// subscriber may read, so apps follow permissioned data without each running
// their own sync of every space and repo.
//
// It is modeled on Jetstream (external/jetstream, internal/subscribe), adapted
// to spaces' permissioned data:
//
//   - The log is the spaces store's event log ([spaces.Store.ListEvents]):
//     every record carries the seq of its latest write, so the log is
//     compacted. Replaying it converges on current state, but intermediate
//     versions are not kept.
//   - Streams are pull-based, as in Jetstream: each reads the log from its
//     own cursor, so a slow reader just falls behind and there is no
//     per-stream buffer. A stream at the head waits on a [Broadcaster] wake
//     from the store, or a poll tick for writes made by other pear instances.
//   - Unlike public data, every event is authorized for its subscriber as it
//     is delivered. A user (OAuth or service auth) receives events from the
//     spaces they currently hold the reader role on, rechecked page by page, so
//     losing access stops the flow. A space credential receives its one space,
//     and the stream ends when the credential expires.
//   - Frames are xrpc.v1.json, like Jetstream v2's subscribeEvents: a message
//     frame per event, payload $type network.habitat.space.subscribe#<kind>,
//     and an error frame before a server-side close.
//
// For a large backlog, [Server.Snapshot] (network.habitat.space.getSnapshot)
// pages the same log over plain HTTP up to a pinned tip, the counterpart of
// Jetstream's replay archive (planSnapshot), and a subscriber then follows
// live from tip + 1.
//
// Delivery is at-least-once and the cursor is inclusive, as in Jetstream.
package spacestream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/spacescope"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Source is the part of [spaces.Store] streams read the event log from.
type Source interface {
	SequenceEvents(ctx context.Context) (spaces.EventSeq, error)
	ListEvents(
		ctx context.Context,
		filter spaces.EventFilter,
		cursor spaces.EventSeq,
		limit int,
	) ([]spaces.Event, error)
}

// RoleChecker resolves whether a user holds a role on a space.
type RoleChecker interface {
	CheckUserHasSpaceRole(
		ctx context.Context,
		did syntax.DID,
		space habitat_syntax.SpaceURI,
		role habitat_syntax.SpaceRole,
	) (bool, error)
}

// Conn is the subscriber's end of a stream.
type Conn interface {
	// WriteFrame sends one text frame.
	WriteFrame(ctx context.Context, frame []byte) error
}

// Reader is who a stream is for, which decides the events it may carry: a
// [UserReader] or a [SpaceCredentialReader].
type Reader interface {
	isReader()
}

// UserReader is a stream authenticated as a user, with OAuth or service auth.
// It carries events from every space the user holds the reader role on that
// its credential's scope also covers: an OAuth token's space: grants, where
// read_self narrows a space to the user's own records.
type UserReader struct {
	DID   syntax.DID
	Scope spacescope.Limit
}

// SpaceCredentialReader is a stream authenticated with a space credential. It
// carries events from that space alone.
type SpaceCredentialReader struct {
	Space habitat_syntax.SpaceURI
}

func (UserReader) isReader()            {}
func (SpaceCredentialReader) isReader() {}

// Subscription is one subscriber's request, already authenticated.
type Subscription struct {
	Query  Query
	Reader Reader
	// ExpiresAt is when the credential the stream was opened with expires,
	// which ends the stream; zero when it doesn't expire.
	ExpiresAt time.Time
}

// ErrCredentialMismatch is returned by [Prepare] when a space credential
// subscriber's spaces filter leaves out the space it can read, so the stream
// could never carry anything.
var ErrCredentialMismatch = errors.New("spaces filter excludes the credential's space")

// errExpired is the cause of a stream's context ending at ExpiresAt.
var errExpired = errors.New("credential expired")

const (
	// DefaultPageSize is how many events a stream reads from the log at once.
	DefaultPageSize = 100
	// DefaultPollInterval is how often a stream at the head rereads the log
	// without a wake, for writes made by another pear instance.
	DefaultPollInterval = 2 * time.Second
)

// Server runs subscribe streams over the event log.
type Server struct {
	source       Source
	roles        RoleChecker
	broadcaster  *Broadcaster
	pageSize     int
	pollInterval time.Duration
}

// Option configures a [Server].
type Option func(*Server)

// WithPageSize sets how many events a stream reads from the log at once.
func WithPageSize(n int) Option {
	return func(s *Server) { s.pageSize = n }
}

// WithPollInterval sets how often a stream at the head rereads the log
// without a wake.
func WithPollInterval(d time.Duration) Option {
	return func(s *Server) { s.pollInterval = d }
}

// NewServer returns a Server reading source, authorizing users against roles,
// and woken by broadcaster, which must be notified of the store's writes.
func NewServer(
	source Source,
	roles RoleChecker,
	broadcaster *Broadcaster,
	opts ...Option,
) *Server {
	s := &Server{
		source:       source,
		roles:        roles,
		broadcaster:  broadcaster,
		pageSize:     DefaultPageSize,
		pollInterval: DefaultPollInterval,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Prepare checks a subscription before its connection is upgraded, so a
// request that can't stream anything is refused with HTTP 400 like
// Jetstream's pre-upgrade errors: a space credential whose spaces filter
// excludes its space is [ErrCredentialMismatch]. It narrows a space
// credential's filter to its space.
func Prepare(sub Subscription) (Subscription, error) {
	cred, ok := sub.Reader.(SpaceCredentialReader)
	if !ok {
		return sub, nil
	}
	if len(sub.Query.Filter.Spaces) > 0 && !slices.Contains(sub.Query.Filter.Spaces, cred.Space) {
		return Subscription{}, ErrCredentialMismatch
	}
	sub.Query.Filter.Spaces = []habitat_syntax.SpaceURI{cred.Space}
	return sub, nil
}

// Run streams the log to conn from the subscription's cursor, then follows it
// live, until ctx ends, conn fails, or the subscription's credential expires.
// On expiry it sends an ExpiredToken error frame and returns nil. sub must
// have been through [Prepare].
func (s *Server) Run(ctx context.Context, conn Conn, sub Subscription) error {
	if !sub.ExpiresAt.IsZero() {
		var cancel context.CancelFunc
		ctx, cancel = context.WithDeadlineCause(ctx, sub.ExpiresAt, errExpired)
		defer cancel()
	}
	err := s.run(ctx, conn, sub)
	if errors.Is(context.Cause(ctx), errExpired) {
		// ctx is done, so the error frame gets a short one of its own.
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		return conn.WriteFrame(writeCtx, errorFrame("ExpiredToken",
			"the credential this stream was opened with has expired"))
	}
	return err
}

func (s *Server) run(ctx context.Context, conn Conn, sub Subscription) error {
	head, err := s.source.SequenceEvents(ctx)
	if err != nil {
		return fmt.Errorf("sequence events: %w", err)
	}
	next := head + 1
	if cursor := sub.Query.Cursor; cursor != nil {
		if *cursor > next {
			// Like Jetstream: a cursor from the future (another host's, say)
			// starts at the live tip, after saying so.
			frame, err := messageFrame(habitat.NetworkHabitatSpaceSubscribeInfo{
				Name: "FutureCursor",
				Message: fmt.Sprintf(
					"cursor %d is beyond the next seq %d; starting at the live tip",
					*cursor, next),
			})
			if err != nil {
				return err
			}
			if err := conn.WriteFrame(ctx, frame); err != nil {
				return err
			}
		} else {
			next = *cursor
		}
	}

	poll := time.NewTicker(s.pollInterval)
	defer poll.Stop()
	for {
		// Taken before reading, so a write during the read still wakes us.
		wake := s.broadcaster.Wait()
		// Called for its side effect: it numbers rows written since the last
		// call, which ListEvents only returns once numbered.
		if _, err := s.source.SequenceEvents(ctx); err != nil {
			return fmt.Errorf("sequence events: %w", err)
		}
		// Scan to the end of the log as it is now, then wait for more.
		next, _, err = s.scan(ctx, sub, scanRange{From: next, To: math.MaxInt64},
			func(event spaces.Event) (bool, error) {
				frame, err := eventFrame(event)
				if err != nil {
					return false, err
				}
				return true, conn.WriteFrame(ctx, frame)
			})
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return context.Cause(ctx)
		case <-wake:
		case <-poll.C:
		}
	}
}

// scanRange bounds a [Server.scan].
type scanRange struct {
	// From is the first seq to read.
	From spaces.EventSeq
	// To is the last seq to read, inclusive.
	To spaces.EventSeq
	// MaxScan, when positive, ends the scan after a page that brings the log
	// entries read, visible or not, to at least this many.
	MaxScan int
}

// scan reads the log over r a page at a time, authorizing each page as the
// subscription's reader and passing the events it may see to visit, in seq
// order. It is the one read-authorize-advance loop behind both subscribe and
// getSnapshot. sub must have been through [Prepare].
//
// visit returns false to end the scan early, after the event it was given.
// scan returns next, the first seq not yet covered, and done, which is true
// when it covered everything through r.To that the log holds, and false when
// visit or r.MaxScan ended it early.
func (s *Server) scan(
	ctx context.Context,
	sub Subscription,
	r scanRange,
	visit func(spaces.Event) (more bool, err error),
) (next spaces.EventSeq, done bool, err error) {
	next = r.From
	scanned := 0
	for next <= r.To {
		events, err := s.source.ListEvents(ctx, sub.Query.Filter, next, s.pageSize)
		if err != nil {
			return next, false, fmt.Errorf("list events: %w", err)
		}
		// A fresh cache per page, so a revoked reader stops receiving
		// events within a page.
		access := newAccessCache(s.roles, sub)
		for _, event := range events {
			if event.EventSeq() > r.To {
				return next, true, nil
			}
			ok, err := access.allows(ctx, event)
			if err != nil {
				return next, false, fmt.Errorf("authorize event: %w", err)
			}
			next = event.EventSeq() + 1
			scanned++
			if !ok {
				continue
			}
			more, err := visit(event)
			if err != nil {
				return next, false, err
			}
			if !more {
				return next, false, nil
			}
		}
		if len(events) < s.pageSize {
			// The filtered log has nothing more through r.To.
			return next, true, nil
		}
		if r.MaxScan > 0 && scanned >= r.MaxScan {
			return next, false, nil
		}
	}
	return next, true, nil
}

// accessCache authorizes one page of events, resolving each space's access
// once. A fresh one per page means a revoked reader stops receiving events
// within a page.
type accessCache struct {
	roles  RoleChecker
	sub    Subscription
	spaces map[habitat_syntax.SpaceURI]bool
}

func newAccessCache(roles RoleChecker, sub Subscription) *accessCache {
	return &accessCache{roles: roles, sub: sub, spaces: map[habitat_syntax.SpaceURI]bool{}}
}

// allows reports whether the subscription may receive event.
func (a *accessCache) allows(ctx context.Context, event spaces.Event) (bool, error) {
	space := event.EventSpace()
	switch reader := a.sub.Reader.(type) {
	case SpaceCredentialReader:
		return space == reader.Space, nil
	case UserReader:
		scope := reader.Scope.ReadAccess(space)
		if scope == spacescope.ReadNone {
			return false, nil
		}
		if _, ok := event.(spaces.SpaceDeletedEvent); ok {
			// A deleted space has no roles left to check. Its deletion goes
			// to subscribers that name it, which already know of it, and to
			// its owner; see the lexicon's spaces parameter.
			return space.SpaceOwner() == reader.DID ||
				slices.Contains(a.sub.Query.Filter.Spaces, space), nil
		}
		if commit, ok := event.(spaces.CommitEvent); ok &&
			scope == spacescope.ReadSelf && commit.Repo != reader.DID {
			return false, nil
		}
		ok, cached := a.spaces[space]
		if cached {
			return ok, nil
		}
		ok, err := a.roles.CheckUserHasSpaceRole(ctx, reader.DID, space,
			habitat_syntax.SpaceRoleReader)
		if err != nil {
			return false, err
		}
		a.spaces[space] = ok
		return ok, nil
	default:
		return false, fmt.Errorf("unknown reader %T", reader)
	}
}

// wireTimeLayout renders event times like Jetstream: RFC 3339 UTC with
// exactly six fractional digits.
const wireTimeLayout = "2006-01-02T15:04:05.000000Z"

func wireTime(t time.Time) string {
	return t.UTC().Format(wireTimeLayout)
}

// eventFrame renders event as an xrpc.v1.json message frame.
func eventFrame(event spaces.Event) ([]byte, error) {
	payload, err := eventObject(event)
	if err != nil {
		return nil, err
	}
	return messageFrame(payload)
}

func eventObject(event spaces.Event) (any, error) {
	switch e := event.(type) {
	case spaces.CommitEvent:
		payload := habitat.NetworkHabitatSpaceSubscribeCommit{
			Seq:   int64(e.Seq),
			Space: e.Space.AtSpace().String(),
			Did:   e.Repo.String(),
			Uri: habitat_syntax.ConstructSpaceRecordURI(
				e.Space, e.Repo, e.Collection, e.Rkey).AtSpace().String(),
			Time:       wireTime(e.Time),
			Rev:        e.Rev.String(),
			Operation:  string(e.Operation),
			Collection: e.Collection.String(),
			Rkey:       e.Rkey.String(),
		}
		if e.Operation != spaces.WriteDelete {
			payload.Record = e.Value
			payload.Cid = e.Cid.String()
		}
		return payload, nil
	case spaces.SpaceDeletedEvent:
		return habitat.NetworkHabitatSpaceSubscribeSpaceDeleted{
			Seq:   int64(e.Seq),
			Space: e.Space.AtSpace().String(),
			Time:  wireTime(e.Time),
		}, nil
	default:
		return nil, fmt.Errorf("unknown event %T", event)
	}
}

// messageFrame wraps a payload, whose MarshalJSON sets its $type, in an
// xrpc.v1.json message frame.
func messageFrame(payload any) ([]byte, error) {
	frame, err := json.Marshal(struct {
		Type    string `json:"$type"`
		Payload any    `json:"payload"`
	}{Type: "message", Payload: payload})
	if err != nil {
		return nil, fmt.Errorf("encode frame: %w", err)
	}
	return frame, nil
}

// errorFrame renders an xrpc.v1.json error frame. The connection closes after
// one is sent.
func errorFrame(name, message string) []byte {
	// json.Marshal of a struct of strings cannot fail.
	frame, _ := json.Marshal(struct {
		Type    string `json:"$type"`
		Error   string `json:"error"`
		Message string `json:"message,omitempty"`
	}{Type: "error", Error: name, Message: message})
	return frame
}
