package fedsearch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/keyval"
	kvtestutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/habitat-network/habitat/internal/reranker"
)

const (
	caller = syntax.DID("did:plc:caller")
	org    = syntax.DID("did:plc:org")
)

// fakeBackend answers after delay with a result per title, or fails with err.
// With block set it answers only when block is closed or its context ends.
type fakeBackend struct {
	id     BackendID
	titles []string
	delay  time.Duration
	err    error
	block  chan struct{}
	panics bool
}

func (b *fakeBackend) ID() BackendID { return b.id }
func (b *fakeBackend) Kind() Kind    { return "fake" }
func (b *fakeBackend) Name() string  { return "Fake " + string(b.id) }

func (b *fakeBackend) Search(ctx context.Context, q Query) ([]Result, error) {
	if b.panics {
		panic("boom")
	}
	wait := b.block
	if wait == nil {
		wait = make(chan struct{})
		timer := time.AfterFunc(b.delay, func() { close(wait) })
		defer timer.Stop()
	}
	select {
	case <-wait:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	if b.err != nil {
		return nil, b.err
	}
	out := make([]Result, len(b.titles))
	for i, title := range b.titles {
		out[i] = Result{Kind: "item", ID: string(b.id) + "/" + title, Title: title}
	}
	return out, nil
}

// fakeResolver lists fixed backends, or fails with err.
type fakeResolver struct {
	kind     Kind
	backends []Backend
	err      error
}

func (r *fakeResolver) Kind() Kind { return r.kind }

func (r *fakeResolver) Backends(context.Context, syntax.DID, syntax.DID) ([]Backend, error) {
	return r.backends, r.err
}

// titleReranker scores each document by the number its title is, so a test
// can say which results win. It fails with err when set.
type titleReranker struct{ err error }

func (r titleReranker) Rerank(
	_ context.Context, _ string, docs []string, _ int,
) ([]reranker.Scored, error) {
	if r.err != nil {
		return nil, r.err
	}
	out := make([]reranker.Scored, len(docs))
	for i, doc := range docs {
		var title string
		_, _ = fmt.Sscan(doc, &title)
		score, _ := strconv.ParseFloat(title, 64)
		out[i] = reranker.Scored{Index: i, Score: score}
	}
	return out, nil
}

// memStore is an in-memory keyval.EncryptedKV that keeps time with
// time.Now, so expiry follows synctest's clock.
type memStore struct {
	mu      sync.Mutex
	entries map[string]memEntry
}

type memEntry struct {
	data    []byte
	expires time.Time
}

func newMemStore() *memStore { return &memStore{entries: map[string]memEntry{}} }

func (s *memStore) PutEncrypted(_ context.Context, key string, value any, ttl time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[key] = memEntry{data: data, expires: time.Now().Add(ttl)}
	return nil
}

func (s *memStore) GetEncrypted(_ context.Context, key string, dst any) error {
	s.mu.Lock()
	e, ok := s.entries[key]
	s.mu.Unlock()
	if !ok || !e.expires.After(time.Now()) {
		return keyval.ErrNotFound
	}
	return json.Unmarshal(e.data, dst)
}

func newService(store keyval.EncryptedKV, backends []Backend, opts ...Option) *Service {
	return NewService(store, []Resolver{&fakeResolver{kind: "fake", backends: backends}}, opts...)
}

func titles(results []Result) []string {
	out := make([]string, len(results))
	for i, r := range results {
		out[i] = r.Title
	}
	return out
}

func statusOf(t *testing.T, page Page, id BackendID) BackendStatus {
	t.Helper()
	for _, b := range page.Backends {
		if b.ID == id {
			return b
		}
	}
	t.Fatalf("no backend %q in page", id)
	return BackendStatus{}
}

func TestServiceRanksAcrossBackends(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		svc := newService(newMemStore(), []Backend{
			&fakeBackend{id: "a", titles: []string{"0.1", "0.9", "0.5"}, delay: time.Second},
			&fakeBackend{id: "b", titles: []string{"0.7", "0.8"}, delay: 2 * time.Second},
		}, WithReranker(titleReranker{}))

		h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
		require.NoError(t, err)
		first, err := h.Next(ctx, NextOptions{Limit: 2, Timeout: 10 * time.Second, WaitAll: true})
		require.NoError(t, err)
		require.Equal(t, []string{"0.9", "0.8"}, titles(first.Results))
		require.Equal(t, RankingReranker, first.Ranking)
		require.Equal(t, StatusOK, statusOf(t, first, "a").Status)
		require.Equal(t, 3, statusOf(t, first, "a").Count)
		require.Equal(t, time.Second, statusOf(t, first, "a").Elapsed)

		second, err := svc.Continue(ctx, caller, first.Cursor, 2, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"0.7", "0.5"}, titles(second.Results))
		// A retried cursor serves the same page again.
		again, err := svc.Continue(ctx, caller, first.Cursor, 2, 0)
		require.NoError(t, err)
		require.Equal(t, titles(second.Results), titles(again.Results))

		last, err := svc.Continue(ctx, caller, second.Cursor, 2, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"0.1"}, titles(last.Results))
		require.Empty(t, last.Cursor, "the mixed stream is done")

		// A backend's own stream holds all of its results, including those
		// the mixed stream already served.
		a, err := svc.Continue(ctx, caller, statusOf(t, first, "a").Cursor, 2, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"0.9", "0.5"}, titles(a.Results))
		require.Equal(t, a.Cursor, statusOf(t, a, "a").Cursor)
		a, err = svc.Continue(ctx, caller, a.Cursor, 2, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"0.1"}, titles(a.Results))
		require.Empty(t, a.Cursor)
		require.Empty(t, statusOf(t, a, "a").Cursor)
	})
}

func TestServiceServesSlowBackendsLater(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		svc := newService(newMemStore(), []Backend{
			&fakeBackend{id: "fast", titles: []string{"0.5", "0.4"}, delay: time.Second},
			&fakeBackend{id: "slow", titles: []string{"0.95"}, delay: 20 * time.Second},
		}, WithReranker(titleReranker{}))

		h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
		require.NoError(t, err)
		start := time.Now()
		first, err := h.Next(ctx, NextOptions{Limit: 1, Timeout: 10 * time.Second, WaitAll: true})
		require.NoError(t, err)
		require.Equal(t, 10*time.Second, time.Since(start), "waits out the timeout")
		require.Equal(t, []string{"0.5"}, titles(first.Results))
		slow := statusOf(t, first, "slow")
		require.Equal(t, StatusPending, slow.Status)
		require.NotEmpty(t, slow.Cursor)
		require.NotEmpty(t, first.Cursor)

		// The slow backend's cursor waits for it, and comes back empty if it
		// still hasn't answered.
		page, err := svc.Continue(ctx, caller, slow.Cursor, 10, 5*time.Second)
		require.NoError(t, err)
		require.Empty(t, page.Results)
		require.Equal(t, slow.Cursor, statusOf(t, page, "slow").Cursor)
		page, err = svc.Continue(ctx, caller, slow.Cursor, 10, 10*time.Second)
		require.NoError(t, err)
		require.Equal(t, []string{"0.95"}, titles(page.Results))
		require.Equal(t, StatusOK, statusOf(t, page, "slow").Status)

		// The late result tops the next mixed page.
		page, err = svc.Continue(ctx, caller, first.Cursor, 10, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"0.95", "0.4"}, titles(page.Results))
		require.Empty(t, page.Cursor)
	})
}

func TestServiceMixedCursorWaitsForFirstResult(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		svc := newService(newMemStore(), []Backend{
			&fakeBackend{id: "a", titles: []string{"0.3"}, delay: 5 * time.Second},
			&fakeBackend{id: "b", titles: []string{"0.6"}, delay: 25 * time.Second},
		}, WithReranker(titleReranker{}))
		h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
		require.NoError(t, err)
		first, err := h.Next(ctx, NextOptions{Timeout: time.Second, WaitAll: true})
		require.NoError(t, err)
		require.Empty(t, first.Results)

		start := time.Now()
		page, err := svc.Continue(ctx, caller, first.Cursor, 10, 20*time.Second)
		require.NoError(t, err)
		require.Equal(t, 4*time.Second, time.Since(start), "returns once a result is ready")
		require.Equal(t, []string{"0.3"}, titles(page.Results))
		require.NotEmpty(t, page.Cursor, "b is still pending")

		page, err = svc.Continue(ctx, caller, page.Cursor, 10, 30*time.Second)
		require.NoError(t, err)
		require.Equal(t, []string{"0.6"}, titles(page.Results))
		require.Empty(t, page.Cursor)
	})
}

func TestServiceReportsBackendFailures(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		svc := NewService(newMemStore(), []Resolver{
			&fakeResolver{kind: "fake", backends: []Backend{
				&fakeBackend{id: "ok", titles: []string{"0.5"}},
				&fakeBackend{id: "broken", err: errors.New("search tool failed")},
				&fakeBackend{id: "signed-out", err: fmt.Errorf("github: %w", ErrNotConnected)},
				&fakeBackend{id: "hung", block: make(chan struct{})},
				&fakeBackend{id: "panics", panics: true},
				&fakeBackend{id: "ok", titles: []string{"duplicate"}},
			}},
			&fakeResolver{kind: "other", err: errors.New("db down")},
		}, WithReranker(titleReranker{}), WithBackendTimeout(20*time.Second))

		h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
		require.NoError(t, err)
		page, err := h.Next(ctx, NextOptions{Timeout: time.Minute, WaitAll: true})
		require.NoError(t, err)
		require.Equal(t, []string{"0.5"}, titles(page.Results))
		require.Len(t, page.Backends, 6, "duplicate ids are searched once")

		want := map[BackendID]BackendError{
			"broken":     {Code: CodeUpstreamError, Message: "search tool failed"},
			"signed-out": {Code: CodeNotConnected, Message: "github: not connected"},
			"hung":       {Code: CodeTimeout, Message: "didn't answer in time"},
			"panics":     {Code: CodeUpstreamError, Message: "backend panicked: boom"},
			"other":      {Code: CodeUpstreamError, Message: "couldn't list other backends"},
		}
		for id, wantErr := range want {
			got := statusOf(t, page, id)
			require.NotNil(t, got.Error, id)
			require.Equal(t, wantErr, *got.Error, id)
			require.Empty(t, got.Cursor, id)
		}
		require.Equal(t, StatusTimeout, statusOf(t, page, "hung").Status)
		require.Equal(t, StatusNotConnected, statusOf(t, page, "signed-out").Status)
		require.Equal(t, StatusError, statusOf(t, page, "broken").Status)
		require.Empty(t, page.Cursor)
	})
}

func TestServiceFallbackRanking(t *testing.T) {
	for name, opts := range map[string][]Option{
		"no reranker":     nil,
		"reranker failed": {WithReranker(titleReranker{err: errors.New("cohere down")})},
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()
				svc := newService(newMemStore(), []Backend{
					&fakeBackend{id: "a", titles: []string{"a1", "a2"}},
					&fakeBackend{id: "b", titles: []string{"b1"}},
				}, opts...)
				h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
				require.NoError(t, err)
				page, err := h.Next(ctx, NextOptions{Timeout: time.Second, WaitAll: true})
				require.NoError(t, err)
				require.Equal(t, []string{"a1", "b1", "a2"}, titles(page.Results))
				require.Equal(t, RankingFallback, page.Ranking)
			})
		})
	}
}

func TestServiceRejectsBadInput(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		svc := newService(newMemStore(), []Backend{
			&fakeBackend{id: "a", titles: []string{"1", "2"}},
		}, WithReranker(titleReranker{}))

		_, err := svc.Start(ctx, caller, org, Query{Text: "  "})
		require.ErrorIs(t, err, ErrEmptyQuery)
		_, err = svc.Start(ctx, caller, org, Query{Text: "q", Limit: MaxBackendLimit + 1})
		require.ErrorIs(t, err, ErrInvalidLimit)

		h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
		require.NoError(t, err)
		page, err := h.Next(ctx, NextOptions{Limit: 1, Timeout: time.Second, WaitAll: true})
		require.NoError(t, err)

		_, err = svc.Continue(ctx, "did:plc:someone-else", page.Cursor, 1, 0)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = svc.Continue(ctx, caller, "not a cursor", 1, 0)
		require.ErrorIs(t, err, ErrInvalidCursor)
		_, err = svc.Continue(ctx, caller, Cursor{Search: "nope"}.Encode(), 1, 0)
		require.ErrorIs(t, err, ErrNotFound)
		_, err = svc.Continue(ctx, caller, Cursor{Search: h.ID(), Offset: 5}.Encode(), 1, 0)
		require.ErrorIs(t, err, ErrInvalidCursor)
		_, err = svc.Continue(ctx, caller, Cursor{Search: h.ID(), Backend: "z"}.Encode(), 1, 0)
		require.ErrorIs(t, err, ErrInvalidCursor)
		_, err = svc.Continue(
			ctx,
			caller,
			Cursor{Search: h.ID(), Offset: 3, Backend: "a"}.Encode(),
			1,
			0,
		)
		require.ErrorIs(t, err, ErrInvalidCursor)
		_, err = svc.Continue(ctx, caller, page.Cursor, MaxPageSize+1, 0)
		require.ErrorIs(t, err, ErrInvalidLimit)
	})
}

func TestServiceExpiresSearches(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		svc := newService(newMemStore(), []Backend{
			&fakeBackend{id: "a", titles: []string{"3", "2", "1"}},
		}, WithReranker(titleReranker{}))
		h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
		require.NoError(t, err)
		page, err := h.Next(ctx, NextOptions{Limit: 1, Timeout: time.Second, WaitAll: true})
		require.NoError(t, err)

		// Idle long enough to be dropped from memory, the search is served
		// from the store, and each use renews it.
		time.Sleep(5 * time.Minute)
		page, err = svc.Continue(ctx, caller, page.Cursor, 1, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"2"}, titles(page.Results))
		time.Sleep(9 * time.Minute)
		page, err = svc.Continue(ctx, caller, page.Cursor, 1, 0)
		require.NoError(t, err)
		require.Equal(t, []string{"1"}, titles(page.Results))

		time.Sleep(DefaultTTL + time.Minute)
		_, err = svc.Continue(ctx, caller, Cursor{Search: h.ID(), Offset: 3}.Encode(), 1, 0)
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestServiceResumesAfterRestart(t *testing.T) {
	ctx := t.Context()
	store := kvtestutil.NewEncrypted(t)
	hung := make(chan struct{})
	backends := []Backend{
		&fakeBackend{id: "a", titles: []string{"2", "1"}},
		&fakeBackend{id: "hung", block: hung},
	}
	svc := newService(store, backends, WithReranker(titleReranker{}))
	h, err := svc.Start(ctx, caller, org, Query{Text: "q"})
	require.NoError(t, err)
	page, err := h.Next(ctx, NextOptions{Limit: 1, Timeout: 10 * time.Second})
	require.NoError(t, err)
	require.Equal(t, []string{"2"}, titles(page.Results))

	restarted := newService(store, backends, WithReranker(titleReranker{}))
	page, err = restarted.Continue(ctx, caller, page.Cursor, 1, time.Second)
	require.NoError(t, err)
	require.Equal(t, []string{"1"}, titles(page.Results))
	require.Equal(t, StatusError, statusOf(t, page, "hung").Status)
	require.Equal(t, CodeInterrupted, statusOf(t, page, "hung").Error.Code)
	require.Empty(t, page.Cursor)

	_, err = restarted.Continue(ctx, "did:plc:someone-else", page.Backends[0].Cursor, 1, 0)
	require.ErrorIs(t, err, ErrNotFound)

	close(hung)
	svc.Wait()
}

func TestCursorRoundTrip(t *testing.T) {
	for _, c := range []Cursor{
		{Search: "id", Offset: 10},
		{Search: "id", Backend: "mcp:github/github-issues"},
	} {
		got, err := DecodeCursor(c.Encode())
		require.NoError(t, err)
		require.Equal(t, c, got)
	}
	for _, bad := range []string{"", "!!", Cursor{}.Encode(), Cursor{Search: "id", Offset: -1}.Encode()} {
		_, err := DecodeCursor(bad)
		require.ErrorIs(t, err, ErrInvalidCursor, bad)
	}
}
