package search

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/meilisearch/meilisearch-go"
	"golang.org/x/sync/errgroup"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// projectionVersion versions how records are turned into documents. Bumping
// it builds a new generation, since documents already indexed were derived
// the old way.
const projectionVersion = 1

const (
	// DefaultKeepGenerations is how many generations older than the current
	// one are kept for rollback.
	DefaultKeepGenerations = 1
	// DefaultCheckInterval is how often a process that doesn't hold the lock
	// checks for a new current generation and for the lock, and how often a
	// building generation is checked for having caught up.
	DefaultCheckInterval = 30 * time.Second
	// minDocRatio is the smallest share of the current generation's documents
	// a new generation must hold to replace it. It only catches a build gone
	// badly wrong, since a projection change can rightly change the count.
	minDocRatio = 0.5
	// stateID is the id of the state document in the generations index.
	stateID = "state"
)

// errFlipped ends leading after a new generation became current, so the
// leader starts over with it.
var errFlipped = errors.New("search generation flipped")

// ErrBuildRunning is returned by [Generations.Build] while another process
// is building a generation.
var ErrBuildRunning = errors.New("another search generation build is running")

// Lock makes sure one process at a time does a piece of the index's work.
// [github.com/habitat-network/habitat/internal/db.Lock] implements it.
type Lock interface {
	// TryAcquire takes the lock if it is free and reports whether it did.
	// When it did, held is cancelled once the lock is released or lost, and
	// release must be called to free it.
	TryAcquire(ctx context.Context) (held context.Context, release func(), ok bool, err error)
}

// generationState is which generations exist: searches read Current, and
// every generation from Min up is kept.
type generationState struct {
	ID      string `json:"id"`
	Current int    `json:"current"`
	Min     int    `json:"min"`
}

// generationInfo records how one generation was built.
type generationInfo struct {
	ID     string `json:"id"`
	Number int    `json:"number"`
	// Spec is the [specHash] the generation was built with; empty for the
	// index that predates generations.
	Spec string `json:"spec"`
}

// Generations keeps the search index as numbered generations, each its own
// pair of Meilisearch indexes, so a change to how the index is built never
// reconfigures the index searches read. Generation n of the index prefix is
// stored in prefix_gn and prefix_gn_cursors; generation 0 is the index from
// before generations, prefix and prefix_cursors. Which generation is current
// is kept in prefix_generations, beside the indexes it describes, so a new
// Meilisearch server starts with no state and builds from scratch.
//
// [Generations.Run] indexes writes into the current generation, in one
// process at a time. A new generation is only built by [Generations.Build],
// which `pear run reindex` calls: it builds the generation beside the
// current one, sweeping every repo into it, and once it has caught up it
// becomes current, and the process running Run moves over to it.
// Generations from the current one less the kept count are kept for
// rollback, and older ones are deleted. A build stopped part way resumes from
// the new generation's cursors when it is run again.
//
// Generations is a [Searchable] over the current generation, and a
// [spaces.Notifier] that forwards to the indexer of the current generation.
type Generations struct {
	client      meilisearch.ServiceManager
	prefix      string
	indexOpts   []MeilisearchOption
	indexerOpts []IndexerOption
	spec        string
	keep        int
	interval    time.Duration
	lock        Lock
	state       meilisearch.IndexManager

	mu         sync.RWMutex
	current    *Meilisearch
	currentNum int
	indexers   []*Indexer
}

var _ Searchable = (*Generations)(nil)

// GenerationsOption configures [Generations].
type GenerationsOption func(*Generations)

// WithIndexOptions configures every generation's index, such as with
// [WithEmbedder].
func WithIndexOptions(opts ...MeilisearchOption) GenerationsOption {
	return func(g *Generations) { g.indexOpts = append(g.indexOpts, opts...) }
}

// WithIndexerOptions configures every generation's indexer, such as with
// [WithBlobs].
func WithIndexerOptions(opts ...IndexerOption) GenerationsOption {
	return func(g *Generations) { g.indexerOpts = append(g.indexerOpts, opts...) }
}

// WithKeepGenerations keeps n generations older than the current one, rather
// than [DefaultKeepGenerations].
func WithKeepGenerations(n int) GenerationsOption {
	return func(g *Generations) { g.keep = n }
}

// WithCheckInterval checks for a new current generation, for the lock and
// on a building generation every d, rather than [DefaultCheckInterval].
func WithCheckInterval(d time.Duration) GenerationsOption {
	return func(g *Generations) { g.interval = d }
}

// NewGenerations returns the generations of the index prefix on the
// Meilisearch server at host, searching the current one. Nothing is built or
// indexed until [Generations.Run].
func NewGenerations(
	ctx context.Context,
	host, apiKey, prefix string,
	lock Lock,
	opts ...GenerationsOption,
) (*Generations, error) {
	client := meilisearch.New(host, meilisearch.WithAPIKey(apiKey))
	g := &Generations{
		client:     client,
		prefix:     prefix,
		keep:       DefaultKeepGenerations,
		interval:   DefaultCheckInterval,
		lock:       lock,
		state:      client.Index(prefix + "_generations"),
		currentNum: -1,
	}
	for _, opt := range opts {
		opt(g)
	}
	spec, err := specHash(g.indexOpts...)
	if err != nil {
		return nil, err
	}
	g.spec = spec
	if err := g.refresh(ctx); err != nil {
		return nil, err
	}
	return g, nil
}

// specHash identifies what a generation is built as: its index settings and
// the projection of records into documents. The embedder's API key is left
// out, since changing it never re-embeds.
func specHash(opts ...MeilisearchOption) (string, error) {
	var m Meilisearch
	for _, opt := range opts {
		opt(&m)
	}
	var embedder *Embedder
	if m.embedder != nil {
		e := *m.embedder
		e.APIKey = ""
		embedder = &e
	}
	b, err := json.Marshal(struct {
		Projection int                   `json:"projection"`
		Settings   *meilisearch.Settings `json:"settings"`
	}{projectionVersion, indexSettings(embedder)})
	if err != nil {
		return "", fmt.Errorf("encode search index spec: %w", err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:8]), nil
}

// uid is the Meilisearch index of generation n's documents.
func (g *Generations) uid(n int) string {
	if n == 0 {
		return g.prefix
	}
	return g.prefix + "_g" + strconv.Itoa(n)
}

func generationID(n int) string { return "g" + strconv.Itoa(n) }

// Current returns the number of the generation searches read, or -1 before
// there is one.
func (g *Generations) Current() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.currentNum
}

func (g *Generations) currentIndex() *Meilisearch {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.current
}

// refresh opens the current generation for searching if it changed. Without
// any state yet, it reads the index from before generations, if there is
// one.
func (g *Generations) refresh(ctx context.Context) error {
	state, found, err := g.loadState(ctx)
	if err != nil {
		return err
	}
	n := state.Current
	if !found {
		exists, err := g.indexExists(ctx, g.uid(0))
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		n = 0
	}
	if n == g.Current() {
		return nil
	}
	idx, err := openMeilisearch(ctx, g.client, g.uid(n), g.indexOpts...)
	if err != nil {
		return err
	}
	g.mu.Lock()
	g.current, g.currentNum = idx, n
	g.mu.Unlock()
	return nil
}

// Run indexes writes into the current generation while this process holds
// the lock, and otherwise follows the current generation for searching, until
// ctx is done. Errors are logged and retried, so Run only returns ctx's
// error.
func (g *Generations) Run(ctx context.Context, source RepoSource, perms PermSource) error {
	for {
		if err := g.refresh(ctx); err != nil {
			slog.ErrorContext(ctx, "search: refresh current generation failed", "err", err)
		}
		held, release, ok, err := g.lock.TryAcquire(ctx)
		if err != nil {
			slog.ErrorContext(ctx, "search: acquire index lock failed", "err", err)
		}
		if ok {
			err := g.lead(held, source, perms)
			release()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, errFlipped) {
				// Start over at once with the new current generation.
				continue
			}
			if err != nil {
				slog.ErrorContext(ctx, "search: maintaining index failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(g.interval):
		}
	}
}

// lead indexes writes into the current generation until ctx is done or
// another generation becomes current.
func (g *Generations) lead(ctx context.Context, source RepoSource, perms PermSource) error {
	state, gens, err := g.ensureState(ctx)
	if err != nil {
		return err
	}
	if err := g.refresh(ctx); err != nil {
		return err
	}
	current := g.currentIndex()
	if current == nil {
		return fmt.Errorf("current search generation %d is missing", state.Current)
	}
	if gens[state.Current].Spec != g.spec {
		slog.WarnContext(ctx, "search: the current generation was built with other settings; "+
			"run `pear run reindex` to build one with these",
			"generation", state.Current)
	}
	eg, egCtx := errgroup.WithContext(ctx)
	x := NewIndexer(current, g.indexerOpts...)
	eg.Go(func() error { return x.Run(egCtx, source, perms) })
	eg.Go(func() error { return g.awaitFlip(egCtx, state.Current) })
	g.setIndexers([]*Indexer{x})
	defer g.setIndexers(nil)
	return eg.Wait()
}

// awaitFlip returns errFlipped once a generation other than n is current.
func (g *Generations) awaitFlip(ctx context.Context, n int) error {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		if err := g.refresh(ctx); err != nil {
			slog.ErrorContext(ctx, "search: refresh current generation failed", "err", err)
			continue
		}
		if g.Current() != n {
			return errFlipped
		}
	}
}

// Build builds a generation with this process's settings and makes it
// current, returning its number once it is. It holds lock, so only one build
// runs at a time, and returns [ErrBuildRunning] if another one holds it.
//
// When the current generation already has these settings there is nothing to
// build unless fresh is set, which rebuilds from scratch anyway. Otherwise a
// kept generation with these settings, such as the one a rolled back deploy
// read, is caught up rather than rebuilt. Either way a build stopped part way
// is resumed.
//
// Build only sweeps repos into the new generation; writes reach it once a
// sweep sees them, and the process running [Generations.Run] indexes them
// into whichever generation is current.
func (g *Generations) Build(
	ctx context.Context,
	lock Lock,
	source RepoSource,
	perms PermSource,
	fresh bool,
) (int, error) {
	held, release, ok, err := lock.TryAcquire(ctx)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrBuildRunning
	}
	defer release()
	ctx = held

	state, gens, err := g.ensureState(ctx)
	if err != nil {
		return 0, err
	}
	if err := g.refresh(ctx); err != nil {
		return 0, err
	}
	target, err := g.target(ctx, state, gens, fresh)
	if err != nil {
		return 0, err
	}
	if err := g.prune(ctx, state, gens, target); err != nil {
		return 0, err
	}
	if target < 0 {
		slog.InfoContext(ctx, "search: the current generation already has these settings",
			"generation", state.Current)
		return state.Current, nil
	}
	current := g.currentIndex()
	if current == nil {
		return 0, fmt.Errorf("current search generation %d is missing", state.Current)
	}
	idx, err := createMeilisearch(ctx, g.client, g.uid(target), g.indexOpts...)
	if err != nil {
		return 0, err
	}
	slog.InfoContext(ctx, "search: building generation",
		"generation", target, "current", state.Current)
	eg, egCtx := errgroup.WithContext(ctx)
	x := NewIndexer(idx, g.indexerOpts...)
	eg.Go(func() error { return x.Run(egCtx, source, perms) })
	eg.Go(func() error { return g.awaitBuild(egCtx, source, x, current, idx, target) })
	if err := eg.Wait(); !errors.Is(err, errFlipped) {
		return 0, err
	}
	if err := g.refresh(ctx); err != nil {
		return 0, err
	}
	// The flip moved the kept range, so drop what fell out of it.
	state, gens, err = g.ensureState(ctx)
	if err != nil {
		return 0, err
	}
	if err := g.prune(ctx, state, gens, -1); err != nil {
		return 0, err
	}
	return target, nil
}

// ensureState returns the generations' state, creating it on first run: the
// index from before generations becomes generation 0, and without one a
// fresh generation 1 becomes current at once, since there is nothing better
// to search while it fills.
func (g *Generations) ensureState(
	ctx context.Context,
) (generationState, map[int]generationInfo, error) {
	state, found, err := g.loadState(ctx)
	if err != nil {
		return generationState{}, nil, err
	}
	if found {
		gens, err := g.loadGenerations(ctx)
		return state, gens, err
	}
	if err := g.setupStateIndex(ctx); err != nil {
		return generationState{}, nil, err
	}
	legacy, err := g.indexExists(ctx, g.uid(0))
	if err != nil {
		return generationState{}, nil, err
	}
	info := generationInfo{ID: generationID(0), Number: 0}
	if !legacy {
		if _, err := createMeilisearch(ctx, g.client, g.uid(1), g.indexOpts...); err != nil {
			return generationState{}, nil, err
		}
		info = generationInfo{ID: generationID(1), Number: 1, Spec: g.spec}
	}
	state = generationState{ID: stateID, Current: info.Number, Min: info.Number}
	if err := g.writeState(ctx, info, state); err != nil {
		return generationState{}, nil, err
	}
	return state, map[int]generationInfo{info.Number: info}, nil
}

// target returns the generation to build so searches can move to one built
// with this process's spec, or -1 when the current one already is and fresh
// is unset. A build above the current generation with the spec, left by a
// build that stopped, is resumed; so is a kept older generation with it, such
// as one left by a rollback, unless fresh is set. Otherwise a new generation
// is numbered after every existing one.
func (g *Generations) target(
	ctx context.Context,
	state generationState,
	gens map[int]generationInfo,
	fresh bool,
) (int, error) {
	if !fresh && gens[state.Current].Spec == g.spec {
		return -1, nil
	}
	target, highest := -1, state.Current
	for n, info := range gens {
		highest = max(highest, n)
		reusable := n > state.Current || (!fresh && n >= state.Min && n != state.Current)
		if reusable && info.Spec == g.spec {
			target = max(target, n)
		}
	}
	if target >= 0 {
		return target, nil
	}
	target = highest + 1
	if err := g.writeInfo(ctx, generationInfo{
		ID: generationID(target), Number: target, Spec: g.spec,
	}); err != nil {
		return 0, err
	}
	return target, nil
}

// prune deletes every generation below the kept minimum, and every build
// above the current generation other than target, whose spec no process
// will build again.
func (g *Generations) prune(
	ctx context.Context,
	state generationState,
	gens map[int]generationInfo,
	target int,
) error {
	for n := range gens {
		if n >= state.Min && (n <= state.Current || n == target) {
			continue
		}
		for _, uid := range []string{g.uid(n), g.uid(n) + "_cursors"} {
			if err := g.deleteIndex(ctx, uid); err != nil {
				return err
			}
		}
		info, err := g.state.DeleteDocumentWithContext(ctx, generationID(n), nil)
		if err != nil {
			return fmt.Errorf("delete search generation %d: %w", n, err)
		}
		if err := waitTask(ctx, g.client, info); err != nil {
			return fmt.Errorf("delete search generation %d: %w", n, err)
		}
		slog.InfoContext(ctx, "search: deleted generation", "generation", n)
	}
	return nil
}

// awaitBuild makes generation n current once its indexer has caught up, and
// then returns errFlipped.
func (g *Generations) awaitBuild(
	ctx context.Context,
	source RepoSource,
	x *Indexer,
	current, built *Meilisearch,
	n int,
) error {
	ticker := time.NewTicker(g.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		ready, err := g.buildReady(ctx, source, x, current, built, n)
		if err != nil {
			slog.ErrorContext(ctx, "search: check generation build failed",
				"generation", n, "err", err)
			continue
		}
		if !ready {
			continue
		}
		state := generationState{ID: stateID, Current: n, Min: max(n-g.keep, 0)}
		if err := g.writeState(ctx, generationInfo{}, state); err != nil {
			return err
		}
		slog.InfoContext(ctx, "search: generation is current", "generation", n)
		return errFlipped
	}
}

// buildReady reports whether generation n has caught up and holds enough
// documents to replace current.
func (g *Generations) buildReady(
	ctx context.Context,
	source RepoSource,
	x *Indexer,
	current, built *Meilisearch,
	n int,
) (bool, error) {
	caughtUp, err := x.CaughtUp(ctx, source)
	if err != nil || !caughtUp {
		return false, err
	}
	have, err := built.count(ctx)
	if err != nil {
		return false, err
	}
	had, err := current.count(ctx)
	if err != nil {
		return false, err
	}
	if float64(have) < minDocRatio*float64(had) {
		slog.WarnContext(ctx, "search: built generation has too few documents to become current",
			"generation", n, "documents", have, "current_documents", had)
		return false, nil
	}
	return true, nil
}

func (g *Generations) setIndexers(indexers []*Indexer) {
	g.mu.Lock()
	g.indexers = indexers
	g.mu.Unlock()
}

func (g *Generations) activeIndexers() []*Indexer {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.indexers
}

// NotifyWrite implements [spaces.Notifier] by forwarding to the indexer of
// the current generation. While this process doesn't hold the lock there is
// none, and the holder's sweep picks the write up, as a build's sweep does.
func (g *Generations) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
	spaceRev, prevSpaceRev syntax.TID,
) {
	for _, x := range g.activeIndexers() {
		x.NotifyWrite(ctx, space, repo, rev, hash, spaceRev, prevSpaceRev)
	}
}

// NotifySpaceDeleted implements [spaces.Notifier] like NotifyWrite.
func (g *Generations) NotifySpaceDeleted(ctx context.Context, space habitat_syntax.SpaceURI) {
	for _, x := range g.activeIndexers() {
		x.NotifySpaceDeleted(ctx, space)
	}
}

// Search implements [Searchable] over the current generation. Before there
// is one it finds nothing.
func (g *Generations) Search(ctx context.Context, q Query) (Result, error) {
	idx := g.currentIndex()
	if idx == nil {
		return Result{}, nil
	}
	return idx.Search(ctx, q)
}

// loadState reads the generations' state, reporting whether there is any.
func (g *Generations) loadState(ctx context.Context) (generationState, bool, error) {
	var state generationState
	err := g.state.GetDocumentWithContext(ctx, stateID, nil, &state)
	if isNotFound(err) {
		return generationState{}, false, nil
	}
	if err != nil {
		return generationState{}, false, fmt.Errorf("get search generation state: %w", err)
	}
	return state, true, nil
}

// loadGenerations reads every generation's info, by number.
func (g *Generations) loadGenerations(ctx context.Context) (map[int]generationInfo, error) {
	var page meilisearch.DocumentsResult
	if err := g.state.GetDocumentsWithContext(ctx, &meilisearch.DocumentsQuery{
		Limit: accessPageSize,
	}, &page); err != nil {
		return nil, fmt.Errorf("list search generations: %w", err)
	}
	var rows []generationInfo
	if err := page.Results.DecodeInto(&rows); err != nil {
		return nil, fmt.Errorf("decode search generations: %w", err)
	}
	gens := map[int]generationInfo{}
	for _, r := range rows {
		if r.ID != stateID {
			gens[r.Number] = r
		}
	}
	return gens, nil
}

// setupStateIndex creates the generations index.
func (g *Generations) setupStateIndex(ctx context.Context) error {
	m := &Meilisearch{client: g.client}
	return m.setup(ctx, g.prefix+"_generations", &meilisearch.Settings{})
}

func (g *Generations) writeInfo(ctx context.Context, info generationInfo) error {
	return g.writeState(ctx, info, generationState{})
}

// writeState writes info and state, skipping either when zero, in one task.
func (g *Generations) writeState(
	ctx context.Context,
	info generationInfo,
	state generationState,
) error {
	var docs []any
	if info.ID != "" {
		docs = append(docs, info)
	}
	if state.ID != "" {
		docs = append(docs, state)
	}
	task, err := g.state.AddDocumentsWithContext(ctx, docs, nil)
	if err != nil {
		return fmt.Errorf("write search generation state: %w", err)
	}
	if err := waitTask(ctx, g.client, task); err != nil {
		return fmt.Errorf("write search generation state: %w", err)
	}
	return nil
}

func (g *Generations) indexExists(ctx context.Context, uid string) (bool, error) {
	_, err := g.client.GetIndexWithContext(ctx, uid)
	if isNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("get meilisearch index %s: %w", uid, err)
	}
	return true, nil
}

// deleteIndex deletes the index uid, if it exists.
func (g *Generations) deleteIndex(ctx context.Context, uid string) error {
	task, err := g.client.DeleteIndexWithContext(ctx, uid)
	if err != nil {
		return fmt.Errorf("delete meilisearch index %s: %w", uid, err)
	}
	err = waitTask(ctx, g.client, task)
	var taskErr *taskError
	if errors.As(err, &taskErr) && taskErr.code == "index_not_found" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete meilisearch index %s: %w", uid, err)
	}
	return nil
}

func isNotFound(err error) bool {
	var apiErr *meilisearch.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// count returns how many documents the index holds.
func (m *Meilisearch) count(ctx context.Context) (int64, error) {
	stats, err := m.index.GetStatsWithContext(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("get meilisearch index stats: %w", err)
	}
	return stats.NumberOfDocuments, nil
}
