package fedsearch

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/reranker"
)

// handleIdle is how long a finished search's handle stays in memory after its
// last use. After that its pages are served from the store, so memory holds
// only searches that are running or being paged through right now.
const handleIdle = time.Minute

// Service starts federated searches and resumes them by cursor.
type Service struct {
	store          keyval.EncryptedKV
	resolvers      []Resolver
	ranker         reranker.Reranker
	backendTimeout time.Duration
	ttl            time.Duration

	// running tracks the backend searches running in the background.
	running sync.WaitGroup

	mu sync.Mutex
	// handles holds the searches with backends still running, which must
	// stay in memory, and those used in the last handleIdle.
	handles map[string]*Search
}

// Option configures a Service.
type Option func(*Service)

// WithReranker scores results with r. Without one, results are scored by
// their rank within their backend.
func WithReranker(r reranker.Reranker) Option {
	return func(s *Service) { s.ranker = r }
}

// WithBackendTimeout sets how long each backend's search may run in the
// background; see DefaultBackendTimeout.
func WithBackendTimeout(d time.Duration) Option {
	return func(s *Service) { s.backendTimeout = d }
}

// WithTTL sets how long a search is kept after its last use; see DefaultTTL.
func WithTTL(d time.Duration) Option {
	return func(s *Service) { s.ttl = d }
}

// NewService returns a Service searching the backends resolvers list, keeping
// searches in store. Give it a store namespaced to it, since keys are search
// ids.
func NewService(store keyval.EncryptedKV, resolvers []Resolver, opts ...Option) *Service {
	s := &Service{
		store:          store,
		resolvers:      resolvers,
		backendTimeout: DefaultBackendTimeout,
		ttl:            DefaultTTL,
		handles:        map[string]*Search{},
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Start starts searching every backend caller has in org for q, in the
// background, and returns the search to page through with [Search.Next].
//
// It fails only for an invalid query or if ctx is done. A resolver that fails
// is reported as an errored backend named after its kind. The backends run
// past ctx, until they answer or the backend timeout passes.
func (s *Service) Start(ctx context.Context, caller, org syntax.DID, q Query) (*Search, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.Text == "" {
		return nil, ErrEmptyQuery
	}
	if q.Limit < 0 || q.Limit > MaxBackendLimit {
		return nil, fmt.Errorf("%w: must be between 1 and %d", ErrInvalidLimit, MaxBackendLimit)
	}
	if q.Limit == 0 {
		q.Limit = DefaultBackendLimit
	}

	backends, failed := s.resolve(ctx, caller, org)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	h := &Search{
		svc: s,
		st: state{
			ID: newSearchID(), Caller: caller, Org: org, Query: q,
			Results: map[BackendID][]Result{}, Scored: map[BackendID]bool{},
			Ranking: RankingReranker,
		},
		changed:  make(chan struct{}),
		lastUsed: time.Now(),
	}
	if s.ranker == nil {
		h.st.Ranking = RankingFallback
	}
	for _, b := range backends {
		h.st.Backends = append(h.st.Backends, BackendState{
			ID: b.ID(), Kind: b.Kind(), Name: b.Name(), Status: StatusPending,
		})
	}
	h.st.Backends = append(h.st.Backends, failed...)
	h.pending = len(backends)
	h.persist(ctx)

	s.mu.Lock()
	s.sweep()
	s.handles[h.st.ID] = h
	s.mu.Unlock()

	for _, b := range backends {
		s.running.Go(func() { h.run(ctx, b) })
	}
	return h, nil
}

// Wait blocks until every backend search running in the background has
// answered or timed out, and its outcome is stored, as before shutdown.
func (s *Service) Wait() {
	s.running.Wait()
}

// resolve lists caller's backends with every resolver at once. It returns the
// backends, with duplicate ids dropped, and an errored state for each
// resolver that failed.
func (s *Service) resolve(
	ctx context.Context,
	caller, org syntax.DID,
) ([]Backend, []BackendState) {
	lists := make([][]Backend, len(s.resolvers))
	errs := make([]error, len(s.resolvers))
	var wg sync.WaitGroup
	for i, r := range s.resolvers {
		wg.Go(func() { lists[i], errs[i] = r.Backends(ctx, caller, org) })
	}
	wg.Wait()

	var (
		backends []Backend
		failed   []BackendState
		seen     = map[BackendID]bool{}
	)
	for i, r := range s.resolvers {
		if errs[i] != nil {
			slog.WarnContext(ctx, "fedsearch: list backends", "kind", r.Kind(), "err", errs[i])
			failed = append(failed, BackendState{
				ID: BackendID(r.Kind()), Kind: r.Kind(), Name: string(r.Kind()),
				Status: StatusError,
				Error: &BackendError{
					Code:    CodeUpstreamError,
					Message: "couldn't list " + string(r.Kind()) + " backends",
				},
			})
			continue
		}
		for _, b := range lists[i] {
			if !seen[b.ID()] {
				seen[b.ID()] = true
				backends = append(backends, b)
			}
		}
	}
	return backends, failed
}

// Get returns caller's search with the given id. It fails with ErrNotFound
// for a search that is unknown, expired or someone else's.
//
// A search no longer in memory is read from the store. If pear restarted
// while its backends ran, those backends are reported as interrupted.
func (s *Service) Get(ctx context.Context, id string, caller syntax.DID) (*Search, error) {
	s.mu.Lock()
	s.sweep()
	h, ok := s.handles[id]
	s.mu.Unlock()
	if ok {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.st.Caller != caller {
			return nil, ErrNotFound
		}
		h.lastUsed = time.Now()
		return h, nil
	}

	var data []byte
	if err := s.store.GetEncrypted(ctx, id, &data); errors.Is(err, keyval.ErrNotFound) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, fmt.Errorf("load search: %w", err)
	}
	var st state
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, fmt.Errorf("decode search: %w", err)
	}
	if st.Caller != caller {
		return nil, ErrNotFound
	}
	// Nothing runs this search's backends any more: this process didn't
	// start them, or it would still hold the handle.
	for i, b := range st.Backends {
		if b.Status == StatusPending {
			st.Backends[i].Status = StatusError
			st.Backends[i].Error = &BackendError{
				Code: CodeInterrupted, Message: "the search was interrupted; search again",
			}
		}
	}
	h = &Search{svc: s, st: st, changed: make(chan struct{}), lastUsed: time.Now()}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another call may have loaded it meanwhile; share one handle, so pages
	// served through either are in one log.
	if existing, ok := s.handles[id]; ok {
		return existing, nil
	}
	s.handles[id] = h
	return h, nil
}

// Continue serves the page cursor points at, of caller's search, waiting up
// to timeout for results; see [Search.Next]. It fails with ErrInvalidCursor
// or ErrNotFound for a cursor that no longer works, after which the client
// should search again.
func (s *Service) Continue(
	ctx context.Context,
	caller syntax.DID,
	cursor string,
	limit int,
	timeout time.Duration,
) (Page, error) {
	c, err := DecodeCursor(cursor)
	if err != nil {
		return Page{}, err
	}
	h, err := s.Get(ctx, c.Search, caller)
	if err != nil {
		return Page{}, err
	}
	return h.Next(ctx, NextOptions{
		Backend: c.Backend, Offset: c.Offset, Limit: limit, Timeout: timeout,
	})
}

// sweep drops the handles of finished searches unused for handleIdle. Their
// state is already in the store. s.mu must be held.
func (s *Service) sweep() {
	cutoff := time.Now().Add(-handleIdle)
	for id, h := range s.handles {
		h.mu.Lock()
		idle := h.pending == 0 && h.lastUsed.Before(cutoff)
		h.mu.Unlock()
		if idle {
			delete(s.handles, id)
		}
	}
}

func newSearchID() string {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf) // never fails, per crypto/rand
	return base64.RawURLEncoding.EncodeToString(buf)
}
