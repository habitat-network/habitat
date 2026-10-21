package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"go.opentelemetry.io/otel/attribute"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"

	"github.com/habitat-network/habitat/internal/telemetry"
)

// DefaultReindexCheckInterval is how often a [Reindexer] checks whether the
// new index has caught up.
const DefaultReindexCheckInterval = 10 * time.Second

// errCaughtUp ends a reindex's indexer once the new index has caught up.
var errCaughtUp = errors.New("search reindex caught up")

// Reindexer rebuilds a [Meilisearch] index with new settings. It builds the
// documents and cursors into uid_next and uid_next_cursors, swaps them with
// uid and uid_cursors once they hold every repo up to its head, and deletes
// the old ones. Searches read the old index until the swap, which is atomic,
// so they never see a partial one.
//
// Writes the server indexes after the new index caught up land in the old
// one, but the new cursors don't cover them, so the server's next sweep
// indexes them again. A reindex stopped part way resumes from uid_next's
// cursors when run again. Never run two at once: they would build into the
// same index and one would delete it from under the other.
type Reindexer struct {
	client      meilisearch.ServiceManager
	uid         string
	indexOpts   []MeilisearchOption
	indexerOpts []IndexerOption
	interval    time.Duration
}

// ReindexerOption configures a [Reindexer].
type ReindexerOption func(*Reindexer)

// WithReindexIndexOptions builds the new index with opts, such as
// [WithEmbedder].
func WithReindexIndexOptions(opts ...MeilisearchOption) ReindexerOption {
	return func(r *Reindexer) { r.indexOpts = append(r.indexOpts, opts...) }
}

// WithReindexIndexerOptions indexes into the new index with opts, such as
// [WithBlobs].
func WithReindexIndexerOptions(opts ...IndexerOption) ReindexerOption {
	return func(r *Reindexer) { r.indexerOpts = append(r.indexerOpts, opts...) }
}

// WithReindexCheckInterval checks whether the new index has caught up every
// d, rather than [DefaultReindexCheckInterval].
func WithReindexCheckInterval(d time.Duration) ReindexerOption {
	return func(r *Reindexer) { r.interval = d }
}

// NewReindexer returns a Reindexer of the index uid on the Meilisearch
// server at host.
func NewReindexer(host, apiKey, uid string, opts ...ReindexerOption) *Reindexer {
	r := &Reindexer{
		client:   meilisearch.New(host, meilisearch.WithAPIKey(apiKey)),
		uid:      uid,
		interval: DefaultReindexCheckInterval,
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Reindex builds the new index from source and perms, swaps it in, and
// deletes the old one.
func (r *Reindexer) Reindex(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
) (err error) {
	next := r.uid + "_next"
	ctx, span := tracer.Start(ctx, "search.Reindex", trace.WithAttributes(
		semconv.DBSystemNameKey.String("meilisearch"),
		semconv.DBCollectionName(r.uid),
	))
	defer func() { telemetry.End(span, err) }()
	start := time.Now()
	idx, err := createMeilisearch(ctx, r.client, next, r.indexOpts...)
	if err != nil {
		return err
	}
	slog.InfoContext(ctx, "search: building new index", "index", next)
	if err := r.build(ctx, source, perms, idx); err != nil {
		return err
	}
	if err := r.swap(ctx, next); err != nil {
		return err
	}
	slog.InfoContext(ctx, "search: swapped in new index", "index", r.uid)
	// After the swap, next holds the old index.
	for _, uid := range []string{next, next + "_cursors"} {
		if err := r.deleteIndex(ctx, uid); err != nil {
			return err
		}
	}
	slog.InfoContext(ctx, "search: reindex done", "index", r.uid, "took", time.Since(start))
	return nil
}

// build indexes into idx until it has caught up.
func (r *Reindexer) build(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	idx *Meilisearch,
) (err error) {
	ctx, span := tracer.Start(ctx, "search.Reindex build")
	defer func() { telemetry.End(span, err) }()
	eg, egCtx := errgroup.WithContext(ctx)
	x := NewIndexer(idx, r.indexerOpts...)
	eg.Go(func() error { return x.Run(egCtx, source, perms) })
	eg.Go(func() error {
		ticker := time.NewTicker(r.interval)
		defer ticker.Stop()
		for {
			select {
			case <-egCtx.Done():
				return egCtx.Err()
			case <-ticker.C:
			}
			caughtUp, err := x.CaughtUp(egCtx, source)
			if err != nil {
				slog.ErrorContext(egCtx, "search: check new index failed", "err", err)
				continue
			}
			if caughtUp {
				return errCaughtUp
			}
			// The backlog is the reindex's progress: it falls to zero as the
			// new index catches up.
			backlog := x.backlog()
			span.AddEvent("search.reindex.progress",
				trace.WithAttributes(attribute.Int("habitat.search.backlog", backlog)))
			slog.InfoContext(egCtx, "search: waiting for new index to catch up",
				"backlog", backlog)
		}
	})
	if err := eg.Wait(); !errors.Is(err, errCaughtUp) {
		return err
	}
	return nil
}

// swap swaps next and its cursors with the live index and its cursors in one
// atomic task. Where the live index doesn't exist yet, next is renamed to it.
func (r *Reindexer) swap(ctx context.Context, next string) (err error) {
	ctx, span := tracer.Start(ctx, "search.Reindex swap")
	defer func() { telemetry.End(span, err) }()
	var swaps []*meilisearch.SwapIndexesParams
	for _, uid := range []string{r.uid, r.uid + "_cursors"} {
		_, err := r.client.GetIndexWithContext(ctx, uid)
		missing := isNotFound(err)
		if err != nil && !missing {
			return fmt.Errorf("get meilisearch index %s: %w", uid, err)
		}
		swaps = append(swaps, &meilisearch.SwapIndexesParams{
			// With rename, Meilisearch renames the first index to the second.
			Indexes: []string{strings.Replace(uid, r.uid, next, 1), uid},
			Rename:  missing,
		})
	}
	task, err := r.client.SwapIndexesWithContext(ctx, swaps)
	if err != nil {
		return fmt.Errorf("swap meilisearch indexes: %w", err)
	}
	if err := waitTask(ctx, r.client, task); err != nil {
		return fmt.Errorf("swap meilisearch indexes: %w", err)
	}
	return nil
}

// deleteIndex deletes the index uid, if it exists.
func (r *Reindexer) deleteIndex(ctx context.Context, uid string) (err error) {
	ctx, span := tracer.Start(ctx, "search.Reindex delete_index",
		trace.WithAttributes(semconv.DBCollectionName(uid)))
	defer func() { telemetry.End(span, err) }()
	task, err := r.client.DeleteIndexWithContext(ctx, uid)
	if err != nil {
		return fmt.Errorf("delete meilisearch index %s: %w", uid, err)
	}
	err = waitTask(ctx, r.client, task)
	var taskErr *taskError
	if errors.As(err, &taskErr) && taskErr.code == "index_not_found" {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete meilisearch index %s: %w", uid, err)
	}
	return nil
}
