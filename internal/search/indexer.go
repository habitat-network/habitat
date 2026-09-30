package search

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// RepoSource is the part of [spaces.Store] the [Indexer] reads records from.
type RepoSource interface {
	ListRepoOps(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		since string,
		limit int,
	) ([]spaces.Record, *spacecommit.SignedCommit, error)
	ListRepoHeads(ctx context.Context) ([]spaces.RepoHead, error)
	CheckSpaceExists(ctx context.Context, uri habitat_syntax.SpaceURI) (bool, error)
}

const (
	// sweepInterval is how often [Indexer.Run] sweeps for repos the index is
	// behind on.
	sweepInterval = 5 * time.Minute
	// opsPageSize is how many ops the indexer pulls per ListRepoOps call.
	opsPageSize = 100
)

// searchCursor records the last revision of a repo the index holds, so the
// indexer only pulls newer ops.
type searchCursor struct {
	Space string `gorm:"primaryKey"`
	Repo  string `gorm:"primaryKey"`
	Rev   string
}

// Indexer keeps an [Index] in step with the records in the spaces store, as
// another consumer of the spaces sync path: it is a [spaces.Notifier], and
// on each notification pulls the repo's ops it hasn't indexed yet with
// ListRepoOps. A periodic sweep catches up on repos whose notifications were
// missed, such as writes made while pear was down, so it also builds the
// index from scratch.
//
// Notifications only mark a repo as pending, so they never block the write
// that sent them. One goroutine, [Indexer.Run], does the indexing: a repo is
// indexed by one goroutine at a time, so its ops are applied in order, and
// repeated notifications for a repo that is still pending are collapsed.
type Indexer struct {
	db    *gorm.DB
	index Index

	mu      sync.Mutex
	order   []job
	pending map[job]bool
	wake    chan struct{}
}

var _ spaces.Notifier = (*Indexer)(nil)

// job is a unit of indexing work. A job with an empty repo deletes a space.
type job struct {
	space habitat_syntax.SpaceURI
	repo  syntax.DID
}

// NewIndexer returns an Indexer that writes to index and keeps its cursors
// in gdb. Nothing is indexed until [Indexer.Run] is called.
func NewIndexer(gdb *gorm.DB, index Index) *Indexer {
	return &Indexer{
		db:      gdb,
		index:   index,
		pending: map[job]bool{},
		wake:    make(chan struct{}, 1),
	}
}

// NotifyWrite implements [spaces.Notifier] by queueing repo to be indexed.
func (x *Indexer) NotifyWrite(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	_ syntax.TID,
	_ []byte,
) {
	x.enqueue(job{space: space, repo: repo})
}

// NotifySpaceDeleted implements [spaces.Notifier] by queueing space's
// documents to be deleted.
func (x *Indexer) NotifySpaceDeleted(_ context.Context, space habitat_syntax.SpaceURI) {
	x.enqueue(job{space: space})
}

func (x *Indexer) enqueue(j job) {
	x.mu.Lock()
	if !x.pending[j] {
		x.pending[j] = true
		x.order = append(x.order, j)
	}
	x.mu.Unlock()
	select {
	case x.wake <- struct{}{}:
	default:
	}
}

// next pops the oldest pending job. A notification that arrives while the
// job runs queues it again, so the repo's new ops are picked up after.
func (x *Indexer) next() (job, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.order) == 0 {
		return job{}, false
	}
	j := x.order[0]
	x.order = x.order[1:]
	delete(x.pending, j)
	return j, true
}

// Run indexes queued repos, reading them from source, until ctx is done. It
// sweeps on start and then every five minutes. Indexing errors are logged
// and retried on the next notification or sweep, so Run only returns ctx's
// error.
func (x *Indexer) Run(ctx context.Context, source RepoSource) error {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	x.sweep(ctx, source)
	for {
		x.drain(ctx, source)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-x.wake:
		case <-ticker.C:
			x.sweep(ctx, source)
		}
	}
}

// drain runs queued jobs until none are left or ctx is done.
func (x *Indexer) drain(ctx context.Context, source RepoSource) {
	for ctx.Err() == nil {
		j, ok := x.next()
		if !ok {
			return
		}
		var err error
		if j.repo == "" {
			err = x.deleteSpace(ctx, j.space)
		} else {
			err = x.indexRepo(ctx, source, j.space, j.repo)
		}
		if err != nil {
			slog.ErrorContext(ctx, "search indexing failed",
				"space", j.space, "repo", j.repo, "err", err)
		}
	}
}

// sweep queues every repo the index is behind on, and the deletion of every
// indexed space that no longer exists.
func (x *Indexer) sweep(ctx context.Context, source RepoSource) {
	if err := x.queueStale(ctx, source); err != nil {
		slog.ErrorContext(ctx, "search index sweep failed", "err", err)
	}
}

func (x *Indexer) queueStale(ctx context.Context, source RepoSource) error {
	heads, err := source.ListRepoHeads(ctx)
	if err != nil {
		return err
	}
	var cursors []searchCursor
	if err := x.db.WithContext(ctx).Find(&cursors).Error; err != nil {
		return fmt.Errorf("list search cursors: %w", err)
	}
	indexed := make(map[job]string, len(cursors))
	for _, c := range cursors {
		indexed[job{space: habitat_syntax.SpaceURI(c.Space), repo: syntax.DID(c.Repo)}] = c.Rev
	}
	// A deleted space's records stay behind as tombstones, so its repos still
	// have heads. Skip those, and delete the space if it was indexed.
	exists := map[habitat_syntax.SpaceURI]bool{}
	spaceExists := func(space habitat_syntax.SpaceURI) (bool, error) {
		ok, checked := exists[space]
		if checked {
			return ok, nil
		}
		ok, err := source.CheckSpaceExists(ctx, space)
		if err != nil {
			return false, err
		}
		exists[space] = ok
		return ok, nil
	}
	for _, h := range heads {
		if indexed[job{space: h.Space, repo: h.Repo}] >= h.Rev.String() {
			continue
		}
		ok, err := spaceExists(h.Space)
		if err != nil {
			return err
		}
		if ok {
			x.enqueue(job{space: h.Space, repo: h.Repo})
		}
	}
	for j := range indexed {
		ok, err := spaceExists(j.space)
		if err != nil {
			return err
		}
		if !ok {
			x.enqueue(job{space: j.space})
		}
	}
	return nil
}

// indexRepo indexes repo's ops since its cursor, page by page, until it
// reaches the repo's head.
func (x *Indexer) indexRepo(
	ctx context.Context,
	source RepoSource,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) error {
	cursor, err := x.cursor(ctx, space, repo)
	if err != nil {
		return err
	}
	for {
		ops, _, err := source.ListRepoOps(ctx, space, repo, cursor, opsPageSize)
		if errors.Is(err, spaces.ErrRevTooFar) {
			// The cursor is ahead of the repo, which happens when a space is
			// deleted and recreated with the same URI. Start the repo over.
			cursor = ""
			continue
		}
		if err != nil {
			return err
		}
		if len(ops) == 0 {
			return nil
		}
		if err := x.apply(ctx, space, repo, ops); err != nil {
			return err
		}
		// Save the cursor only after the index write, so a crash in between
		// replays the page rather than skipping it.
		cursor = ops[len(ops)-1].Rev
		if err := x.saveCursor(ctx, space, repo, cursor); err != nil {
			return err
		}
		if len(ops) < opsPageSize {
			return nil
		}
	}
}

// apply writes one page of ops to the index. The oplog holds one op per
// record, its latest state, so a page never touches a record twice.
func (x *Indexer) apply(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	ops []spaces.Record,
) error {
	var docs []Document
	var deleted []habitat_syntax.SpaceRecordURI
	for _, op := range ops {
		if habitat_syntax.ReservedCollections.Contains(op.Collection) {
			continue
		}
		uri := habitat_syntax.ConstructSpaceRecordURI(space, repo, op.Collection, op.Rkey)
		if op.Value == nil {
			// A tombstone: the record was deleted.
			deleted = append(deleted, uri)
			continue
		}
		docs = append(docs, Document{
			URI:        uri,
			Space:      space,
			Repo:       repo,
			Collection: op.Collection,
			Rev:        syntax.TID(op.Rev),
			Text:       ExtractText(op.Value),
		})
	}
	if err := x.index.Put(ctx, docs...); err != nil {
		return err
	}
	return x.index.Delete(ctx, deleted...)
}

// deleteSpace removes space's documents and cursors.
func (x *Indexer) deleteSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	if err := x.index.DeleteSpace(ctx, space); err != nil {
		return err
	}
	return x.db.WithContext(ctx).
		Where("space = ?", space.String()).
		Delete(&searchCursor{}).Error
}

func (x *Indexer) cursor(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) (string, error) {
	var c searchCursor
	err := x.db.WithContext(ctx).
		Where("space = ? AND repo = ?", space.String(), repo.String()).
		Limit(1).Find(&c).Error
	if err != nil {
		return "", fmt.Errorf("load search cursor: %w", err)
	}
	return c.Rev, nil
}

func (x *Indexer) saveCursor(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev string,
) error {
	err := x.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "space"}, {Name: "repo"}},
		DoUpdates: clause.AssignmentColumns([]string{"rev"}),
	}).Create(&searchCursor{Space: space.String(), Repo: repo.String(), Rev: rev}).Error
	if err != nil {
		return fmt.Errorf("save search cursor: %w", err)
	}
	return nil
}
