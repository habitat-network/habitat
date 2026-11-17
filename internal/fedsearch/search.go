package fedsearch

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

const (
	// DefaultPageSize is how many results a page holds when NextOptions
	// gives no limit.
	DefaultPageSize = 10
	// MaxPageSize is the largest page a call can ask for.
	MaxPageSize = 100
	// persistTimeout bounds each write of a search to the store.
	persistTimeout = 5 * time.Second
	// maxErrorRunes caps a backend error's message.
	maxErrorRunes = 300
	// fallbackRankOffset is the k of reciprocal rank scoring, 1/(k+rank),
	// used when there's no reranker score.
	fallbackRankOffset = 60
)

// state is everything about a search that is kept in the store.
type state struct {
	ID       string         `json:"id"`
	Caller   syntax.DID     `json:"caller"`
	Org      syntax.DID     `json:"org"`
	Query    Query          `json:"query"`
	Backends []BackendState `json:"backends"`
	// Results holds each backend's results once it has answered: in its own
	// order until scored, then by score, highest first.
	Results map[BackendID][]Result `json:"results"`
	// Scored marks the backends whose results are scored and sorted. Only
	// scored results are served.
	Scored map[BackendID]bool `json:"scored"`
	// Served is the mixed stream so far, in the order it was served. Because
	// it always takes the best unserved result and each backend's results
	// are sorted, it holds a prefix of each backend's results.
	Served  []resultRef `json:"served"`
	Ranking Ranking     `json:"ranking"`
}

// resultRef is a result by its backend and index in that backend's results.
type resultRef struct {
	Backend BackendID `json:"b"`
	Index   int       `json:"i"`
}

// Search is one federated search, running or finished. It is safe for
// concurrent use.
type Search struct {
	svc *Service

	mu      sync.Mutex
	st      state
	version uint64 // bumped on every change to st
	pending int    // backends still running in this process
	// changed is closed, and replaced, whenever a backend answers, to wake
	// callers waiting on it.
	changed  chan struct{}
	lastUsed time.Time

	// scoreMu serializes scoring, so a backend's results are scored once.
	scoreMu sync.Mutex
	// persistMu serializes writes to the store; persisted is the version
	// last written, so an older state never overwrites a newer one.
	persistMu sync.Mutex
	persisted uint64
}

// NextOptions selects a page of a search.
type NextOptions struct {
	// Backend selects that backend's stream; empty is the mixed stream.
	Backend BackendID
	// Offset is how many results of the stream the client already has.
	Offset int
	// Limit is the page size; zero means DefaultPageSize.
	Limit int
	// Timeout is how long to wait for results before serving what's ready,
	// capped just past the backend timeout. Zero serves what's ready now.
	Timeout time.Duration
	// WaitAll waits, up to Timeout, for every backend rather than for the
	// first results. Use it for a search's first page.
	WaitAll bool
}

// Page is one page of a search.
type Page struct {
	Results []Result
	// Backends is every backend's state, with its cursor.
	Backends []BackendStatus
	// Offset is the stream's offset after this page.
	Offset int
	// Cursor continues this page's stream; empty when the stream is done.
	Cursor  string
	Ranking Ranking
}

// BackendStatus is a backend's state and the cursor to its own stream, which
// is empty when the stream has nothing more to serve.
type BackendStatus struct {
	BackendState
	Cursor string
}

// ID is the search's id.
func (h *Search) ID() string { return h.st.ID }

// Next serves a page of one of the search's streams.
//
// It first waits, up to opts.Timeout: on the mixed stream, until a result is
// waiting to be served or every backend has answered (with WaitAll, only the
// latter); on a backend's stream, until that backend has answered. Then it
// scores what has arrived and serves the page.
//
// The mixed stream serves the best results not yet served, by score, and
// remembers them, so an offset already served is served again the same way.
// A backend's stream is that backend's results by score, independent of the
// mixed stream.
func (h *Search) Next(ctx context.Context, opts NextOptions) (Page, error) {
	if opts.Limit < 0 || opts.Limit > MaxPageSize {
		return Page{}, fmt.Errorf(
			"%w: page size must be between 1 and %d",
			ErrInvalidLimit,
			MaxPageSize,
		)
	}
	if opts.Limit == 0 {
		opts.Limit = DefaultPageSize
	}
	// A second past the backend timeout, so a caller asking to wait as long
	// as it takes sees every backend's outcome rather than racing the last.
	opts.Timeout = min(max(opts.Timeout, 0), h.svc.backendTimeout+time.Second)
	if opts.Offset < 0 {
		return Page{}, ErrInvalidCursor
	}
	h.mu.Lock()
	known := opts.Backend == "" || h.backend(opts.Backend) >= 0
	h.mu.Unlock()
	if !known {
		return Page{}, fmt.Errorf("%w: no backend %q", ErrInvalidCursor, opts.Backend)
	}

	if err := h.wait(ctx, opts); err != nil {
		return Page{}, err
	}
	h.score(ctx)

	h.mu.Lock()
	page, err := h.serve(opts)
	h.lastUsed = time.Now()
	h.mu.Unlock()
	if err != nil {
		return Page{}, err
	}
	// Written on every use, to renew its expiry.
	h.persist(ctx)
	return page, nil
}

// wait blocks until opts' condition holds or its timeout passes.
func (h *Search) wait(ctx context.Context, opts NextOptions) error {
	timer := time.NewTimer(opts.Timeout)
	defer timer.Stop()
	for {
		h.mu.Lock()
		done := h.ready(opts)
		changed := h.changed
		h.mu.Unlock()
		if done {
			return nil
		}
		select {
		case <-changed:
		case <-timer.C:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// ready reports whether opts' wait condition holds. h.mu must be held.
func (h *Search) ready(opts NextOptions) bool {
	switch {
	case h.pending == 0:
		return true
	case opts.Backend != "":
		return h.st.Backends[h.backend(opts.Backend)].Status != StatusPending
	case opts.WaitAll:
		return false
	default:
		total := 0
		for _, rs := range h.st.Results {
			total += len(rs)
		}
		return total > len(h.st.Served)
	}
}

// run searches b, records its outcome and stores the search.
func (h *Search) run(ctx context.Context, b Backend) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), h.svc.backendTimeout)
	defer cancel()
	start := time.Now()
	results, err := search(ctx, b, h.st.Query)
	elapsed := time.Since(start)

	h.mu.Lock()
	s := &h.st.Backends[h.backend(b.ID())]
	s.Elapsed = elapsed
	if err != nil {
		slog.WarnContext(ctx, "fedsearch: backend search", "backend", b.ID(), "err", err)
		s.Status, s.Error = classify(ctx, err)
	} else {
		results = results[:min(len(results), h.st.Query.Limit)]
		for i := range results {
			results[i].Backend = b.ID()
			results[i].Score = 0
		}
		s.Status, s.Count = StatusOK, len(results)
		h.st.Results[b.ID()] = results
		if len(results) == 0 {
			h.st.Scored[b.ID()] = true
		}
	}
	h.pending--
	h.version++
	close(h.changed)
	h.changed = make(chan struct{})
	h.mu.Unlock()

	h.persist(ctx)
}

// search calls b, turning a panic into an error so one backend can't take
// down pear.
func search(ctx context.Context, b Backend, q Query) (results []Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("backend panicked: %v", r)
		}
	}()
	return b.Search(ctx, q)
}

// classify maps a backend's failure to its status. ctx is the backend's own
// context: past its deadline, any failure is a timeout.
func classify(ctx context.Context, err error) (Status, *BackendError) {
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded):
		return StatusTimeout, &BackendError{Code: CodeTimeout, Message: "didn't answer in time"}
	case errors.Is(err, ErrNotConnected):
		return StatusNotConnected, &BackendError{
			Code:    CodeNotConnected,
			Message: truncate(err.Error()),
		}
	default:
		return StatusError, &BackendError{Code: CodeUpstreamError, Message: truncate(err.Error())}
	}
}

// score scores the results of every backend that has answered since the
// last call, in one reranker call, and sorts each backend's results by score.
// If the reranker fails, or there is none, the results are scored by their
// rank within their backend.
func (h *Search) score(ctx context.Context) {
	h.scoreMu.Lock()
	defer h.scoreMu.Unlock()

	h.mu.Lock()
	var (
		refs []resultRef
		docs []string
	)
	for _, b := range h.st.Backends {
		if h.st.Scored[b.ID] {
			continue
		}
		for i, r := range h.st.Results[b.ID] {
			refs = append(refs, resultRef{Backend: b.ID, Index: i})
			docs = append(docs, r.Title+"\n"+r.Snippet)
		}
	}
	query := h.st.Query.Text
	h.mu.Unlock()
	if len(refs) == 0 {
		return
	}

	scores := make([]float64, len(refs))
	fallback := h.svc.ranker == nil
	if !fallback {
		scored, err := h.svc.ranker.Rerank(ctx, query, docs, 0)
		if err != nil {
			slog.WarnContext(ctx, "fedsearch: rerank", "err", err)
			fallback = true
		}
		for _, s := range scored {
			if s.Index >= 0 && s.Index < len(scores) {
				scores[s.Index] = s.Score
			}
		}
	}
	if fallback {
		for i, ref := range refs {
			scores[i] = 1 / float64(fallbackRankOffset+ref.Index+1)
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for i, ref := range refs {
		h.st.Results[ref.Backend][ref.Index].Score = scores[i]
		h.st.Scored[ref.Backend] = true
	}
	for b := range h.st.Scored {
		slices.SortStableFunc(h.st.Results[b], func(x, y Result) int {
			return cmp.Compare(y.Score, x.Score)
		})
	}
	if fallback {
		h.st.Ranking = RankingFallback
	}
	h.version++
}

// serve cuts the page opts selects. h.mu must be held.
func (h *Search) serve(opts NextOptions) (Page, error) {
	page := Page{Ranking: h.st.Ranking, Results: []Result{}}
	more := false
	if opts.Backend == "" {
		if opts.Offset > len(h.st.Served) {
			return Page{}, fmt.Errorf("%w: offset past the results served", ErrInvalidCursor)
		}
		for _, ref := range h.st.Served[opts.Offset:min(opts.Offset+opts.Limit, len(h.st.Served))] {
			page.Results = append(page.Results, h.st.Results[ref.Backend][ref.Index])
		}
		for len(page.Results) < opts.Limit {
			ref, ok := h.best()
			if !ok {
				break
			}
			h.st.Served = append(h.st.Served, ref)
			h.version++
			page.Results = append(page.Results, h.st.Results[ref.Backend][ref.Index])
		}
		page.Offset = opts.Offset + len(page.Results)
		_, unserved := h.best()
		more = h.pending > 0 || unserved || page.Offset < len(h.st.Served)
	} else {
		results := h.st.Results[opts.Backend]
		if !h.st.Scored[opts.Backend] {
			results = nil
		}
		if opts.Offset > len(results) && h.st.Scored[opts.Backend] {
			return Page{}, fmt.Errorf("%w: offset past the backend's results", ErrInvalidCursor)
		}
		if opts.Offset <= len(results) {
			page.Results = append(
				page.Results,
				results[opts.Offset:min(opts.Offset+opts.Limit, len(results))]...)
		}
		page.Offset = opts.Offset + len(page.Results)
		pending := h.st.Backends[h.backend(opts.Backend)].Status == StatusPending
		more = pending || page.Offset < len(results)
	}
	if more {
		page.Cursor = Cursor{Search: h.st.ID, Offset: page.Offset, Backend: opts.Backend}.Encode()
	}

	for _, b := range h.st.Backends {
		status := BackendStatus{BackendState: b}
		offset := 0
		if b.ID == opts.Backend {
			offset = page.Offset
		}
		if b.Status == StatusPending || offset < len(h.st.Results[b.ID]) {
			status.Cursor = Cursor{Search: h.st.ID, Offset: offset, Backend: b.ID}.Encode()
		}
		page.Backends = append(page.Backends, status)
	}
	return page, nil
}

// best returns the highest scored result the mixed stream hasn't served,
// preferring the earlier backend on a tie. h.mu must be held.
func (h *Search) best() (resultRef, bool) {
	served := map[BackendID]int{}
	for _, ref := range h.st.Served {
		served[ref.Backend]++
	}
	var (
		best  resultRef
		score float64
		found bool
	)
	for _, b := range h.st.Backends {
		results := h.st.Results[b.ID]
		i := served[b.ID]
		if !h.st.Scored[b.ID] || i >= len(results) {
			continue
		}
		if !found || results[i].Score > score {
			best, score, found = resultRef{Backend: b.ID, Index: i}, results[i].Score, true
		}
	}
	return best, found
}

// backend returns the index of id in the search's backends, or -1. h.mu must
// be held.
func (h *Search) backend(id BackendID) int {
	return slices.IndexFunc(h.st.Backends, func(b BackendState) bool { return b.ID == id })
}

// persist writes the search to the store, unless a newer state is already
// written. A failure is logged: the search goes on in memory, and only a
// cursor used after its handle is dropped would miss it.
func (h *Search) persist(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), persistTimeout)
	defer cancel()
	h.persistMu.Lock()
	defer h.persistMu.Unlock()

	h.mu.Lock()
	version := h.version
	data, err := json.Marshal(h.st)
	h.mu.Unlock()
	if err != nil {
		slog.ErrorContext(ctx, "fedsearch: encode search", "err", err)
		return
	}
	// Writing the same version again still renews the expiry.
	if version < h.persisted {
		return
	}
	if err := h.svc.store.PutEncrypted(ctx, h.st.ID, data, h.svc.ttl); err != nil {
		slog.ErrorContext(ctx, "fedsearch: store search", "err", err)
		return
	}
	h.persisted = version
}

// truncate shortens s to at most maxErrorRunes runes.
func truncate(s string) string {
	if utf8.RuneCountInString(s) <= maxErrorRunes {
		return s
	}
	return string([]rune(s)[:maxErrorRunes-1]) + "…"
}
