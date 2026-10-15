package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
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
	GetRecord(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		owner syntax.DID,
		collection syntax.NSID,
		rkey syntax.RecordKey,
	) (*spaces.Record, error)
}

// PermSource is the part of [perms.Store] the [Indexer] resolves read access
// with.
type PermSource interface {
	// ListUserSubjects lists the users holding role on space, flattening
	// spaceRelations.
	ListUserSubjects(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		role habitat_syntax.SpaceRole,
	) ([]syntax.DID, error)
	// ListDependentSpaces lists the spaces whose roles are granted, through
	// spaceRelations, to holders of a role on space.
	ListDependentSpaces(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) ([]habitat_syntax.SpaceURI, error)
}

const (
	// sweepInterval is how often [Indexer.Run] sweeps for repos the index is
	// behind on.
	sweepInterval = 5 * time.Minute
	// opsPageSize is how many ops the indexer pulls per ListRepoOps call.
	opsPageSize = 100
	// accessCollection holds a space's community.opensocial.access record.
	accessCollection = syntax.NSID("community.opensocial.access")
)

// accessCollections are the collections whose records decide who may read a
// space. A write to one of them changes the space's [Access].
var accessCollections = map[syntax.NSID]bool{
	habitat_syntax.UserRelationCollection:  true,
	habitat_syntax.SpaceRelationCollection: true,
	accessCollection:                       true,
}

// Indexer keeps an [Index] in step with the records in the spaces store, as
// another consumer of the spaces sync path: it is a [spaces.Notifier], and
// on each notification pulls the repo's ops it hasn't indexed yet with
// ListRepoOps. A periodic sweep catches up on repos whose notifications were
// missed, such as writes made while pear was down, so it also builds the
// index from scratch.
//
// Each document is indexed with its space's [Access]. When a write touches a
// space's permission records, the indexer recomputes the Access of that space
// and of every space that grants roles to its role holders, and rewrites
// their documents.
//
// Notifications only mark a repo as pending, so they never block the write
// that sent them. One goroutine, [Indexer.Run], does the indexing: a repo is
// indexed by one goroutine at a time, so its ops are applied in order, and
// repeated notifications for a repo that is still pending are collapsed.
type Indexer struct {
	index Index
	// blobs, when set, supplies the content of blobs records reference; see
	// [WithBlobs].
	blobs BlobSource

	mu      sync.Mutex
	order   []job
	pending map[job]bool
	// running counts jobs popped from the queue that haven't finished.
	running int
	wake    chan struct{}

	// orgConfigs caches each org's collection configs. Only Run's goroutine
	// uses it.
	orgConfigs map[syntax.DID]map[syntax.NSID]CollectionConfig
}

var _ spaces.Notifier = (*Indexer)(nil)

// jobKind is what a [job] does to its space.
type jobKind int

const (
	// jobIndexRepo indexes the job's repo.
	jobIndexRepo jobKind = iota
	// jobRefreshAccess recomputes the space's Access.
	jobRefreshAccess
	// jobDeleteSpace removes the space from the index.
	jobDeleteSpace
	// jobReproject indexes again every repo in the spaces owned by the
	// owner of the job's space, after that org's configs changed.
	jobReproject
)

// job is a unit of indexing work.
type job struct {
	kind  jobKind
	space habitat_syntax.SpaceURI
	// repo is only set for jobIndexRepo.
	repo syntax.DID
}

// IndexerOption configures an [Indexer].
type IndexerOption func(*Indexer)

// NewIndexer returns an Indexer that writes to index. Nothing is indexed
// until [Indexer.Run] is called.
func NewIndexer(index Index, opts ...IndexerOption) *Indexer {
	x := &Indexer{
		index:      index,
		pending:    map[job]bool{},
		wake:       make(chan struct{}, 1),
		orgConfigs: map[syntax.DID]map[syntax.NSID]CollectionConfig{},
	}
	for _, opt := range opts {
		opt(x)
	}
	return x
}

// NotifyWrite implements [spaces.Notifier] by queueing repo to be indexed.
func (x *Indexer) NotifyWrite(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	_ syntax.TID,
	_ []byte,
	_ syntax.TID,
	_ syntax.TID,
) {
	x.enqueue(job{kind: jobIndexRepo, space: space, repo: repo})
}

// NotifySpaceDeleted implements [spaces.Notifier] by queueing space's
// documents to be deleted.
func (x *Indexer) NotifySpaceDeleted(_ context.Context, space habitat_syntax.SpaceURI) {
	x.enqueue(job{kind: jobDeleteSpace, space: space})
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
	x.running++
	return j, true
}

// done marks a job popped by next as finished.
func (x *Indexer) done() {
	x.mu.Lock()
	x.running--
	x.mu.Unlock()
}

// idle reports whether no job is queued or running.
func (x *Indexer) idle() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.order) == 0 && x.running == 0
}

// CaughtUp reports whether the index holds every repo up to its head and no
// space that was deleted, with nothing left to do: what a new index must
// reach before searches move to it. When it hasn't, it queues what is left,
// so an index that misses notifications, such as one a reindex builds,
// catches up without waiting for the next sweep.
func (x *Indexer) CaughtUp(ctx context.Context, source RepoSource) (bool, error) {
	if !x.idle() {
		return false, nil
	}
	stale, deleted, err := x.staleWork(ctx, source)
	if err != nil {
		return false, err
	}
	for _, j := range stale {
		x.enqueue(j)
	}
	for _, space := range deleted {
		x.enqueue(job{kind: jobDeleteSpace, space: space})
	}
	// The queue may have filled while the index was compared, but then the
	// next check sees it.
	return len(stale) == 0 && len(deleted) == 0 && x.idle(), nil
}

// Run indexes queued repos, reading records from source and permissions from
// perms, until ctx is done. It sweeps on start and then every five minutes.
// Indexing errors are logged and retried on the next notification or sweep,
// so Run only returns ctx's error.
//
// Only the collections configs returns for a space's owner are indexed, each
// as its config says, and a write to an org's configs reindexes that org's
// repos when they changed. A nil configs indexes every collection with
// [DefaultConfig].
func (x *Indexer) Run(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	configs ConfigSource,
) error {
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	x.sweep(ctx, source)
	for {
		x.drain(ctx, source, perms, configs)
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
func (x *Indexer) drain(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	configs ConfigSource,
) {
	for ctx.Err() == nil {
		j, ok := x.next()
		if !ok {
			return
		}
		var err error
		switch j.kind {
		case jobIndexRepo:
			err = x.indexRepo(ctx, source, perms, configs, j.space, j.repo)
		case jobRefreshAccess:
			err = x.refreshAccess(ctx, source, perms, j.space)
		case jobDeleteSpace:
			err = x.index.DeleteSpace(ctx, j.space)
		case jobReproject:
			err = x.reproject(ctx, j.space.SpaceOwner())
		}
		x.done()
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
	stale, deleted, err := x.staleWork(ctx, source)
	if err != nil {
		return err
	}
	for _, j := range stale {
		x.enqueue(j)
	}
	for _, space := range deleted {
		x.enqueue(job{kind: jobDeleteSpace, space: space})
	}
	return nil
}

// staleWork returns a job for every repo the index is behind on, and every
// indexed space that no longer exists.
func (x *Indexer) staleWork(
	ctx context.Context,
	source RepoSource,
) ([]job, []habitat_syntax.SpaceURI, error) {
	heads, err := source.ListRepoHeads(ctx)
	if err != nil {
		return nil, nil, err
	}
	cursors, err := x.index.Cursors(ctx)
	if err != nil {
		return nil, nil, err
	}
	type repoKey struct {
		space habitat_syntax.SpaceURI
		repo  syntax.DID
	}
	indexed := make(map[repoKey]string, len(cursors))
	for _, c := range cursors {
		indexed[repoKey{c.Space, c.Repo}] = c.Rev
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
	var stale []job
	for _, h := range heads {
		if indexed[repoKey{h.Space, h.Repo}] >= h.Rev.String() {
			continue
		}
		ok, err := spaceExists(h.Space)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			stale = append(stale, job{kind: jobIndexRepo, space: h.Space, repo: h.Repo})
		}
	}
	var deleted []habitat_syntax.SpaceURI
	seen := map[habitat_syntax.SpaceURI]bool{}
	for k := range indexed {
		if seen[k.space] {
			continue
		}
		seen[k.space] = true
		ok, err := spaceExists(k.space)
		if err != nil {
			return nil, nil, err
		}
		if !ok {
			deleted = append(deleted, k.space)
		}
	}
	return stale, deleted, nil
}

// indexRepo indexes repo's ops since its cursor, page by page, until it
// reaches the repo's head.
func (x *Indexer) indexRepo(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	configs ConfigSource,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
) error {
	cursor, err := x.index.Cursor(ctx, space, repo)
	if err != nil {
		return err
	}
	// Loaded on the first page with documents, and again after a page that
	// changes it.
	var access *Access
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
		if changesAccess(ops) {
			// Apply the new access to the space's existing documents, and to
			// the spaces that inherit roles from it.
			access = nil
			if err := x.queueAccessRefresh(ctx, perms, space); err != nil {
				return err
			}
		}
		if access == nil && hasDocuments(ops) {
			a, err := spaceAccess(ctx, source, perms, space)
			if err != nil {
				return err
			}
			access = &a
		}
		if err := x.apply(ctx, configs, space, repo, ops, access); err != nil {
			return err
		}
		// Save the cursor only after the index write, so a crash in between
		// replays the page rather than skipping it.
		cursor = ops[len(ops)-1].Rev
		if err := x.index.SetCursor(
			ctx,
			RepoCursor{Space: space, Repo: repo, Rev: cursor},
		); err != nil {
			return err
		}
		if changesConfigs(space, ops) {
			if err := x.refreshConfigs(ctx, configs, space); err != nil {
				return err
			}
		}
		if len(ops) < opsPageSize {
			return nil
		}
	}
}

// changesConfigs reports whether ops in space write its owner's configs,
// which live in the owner's members space.
func changesConfigs(space habitat_syntax.SpaceURI, ops []spaces.Record) bool {
	if space.SpaceType() != opensocial.MembersSpaceType {
		return false
	}
	for _, op := range ops {
		if op.Collection == ConfigCollection {
			return true
		}
	}
	return false
}

// configsFor returns org's configs, nil when configs is nil.
func (x *Indexer) configsFor(
	ctx context.Context,
	configs ConfigSource,
	org syntax.DID,
) (map[syntax.NSID]CollectionConfig, error) {
	if configs == nil {
		return nil, nil
	}
	if cfgs, ok := x.orgConfigs[org]; ok {
		return cfgs, nil
	}
	cfgs, err := configs.Configs(ctx, org)
	if err != nil {
		return nil, fmt.Errorf("load search configs: %w", err)
	}
	x.orgConfigs[org] = cfgs
	return cfgs, nil
}

// refreshConfigs reloads the configs of space's owner after a write to them,
// and queues its repos to be indexed again when they changed. Reindexing
// replays the write, but then the configs are unchanged, so it stops there.
//
// Configs not yet loaded are taken as unchanged: documents indexed before
// they were loaded may be from configs that changed while the index was
// behind, which `pear run reindex` brings up to date.
func (x *Indexer) refreshConfigs(
	ctx context.Context,
	configs ConfigSource,
	space habitat_syntax.SpaceURI,
) error {
	if configs == nil {
		return nil
	}
	org := space.SpaceOwner()
	old, cached := x.orgConfigs[org]
	cfgs, err := configs.Configs(ctx, org)
	if err != nil {
		return fmt.Errorf("load search configs: %w", err)
	}
	x.orgConfigs[org] = cfgs
	if cached && !sameConfigs(old, cfgs) {
		x.enqueue(job{kind: jobReproject, space: space})
	}
	return nil
}

// sameConfigs reports whether a and b hold the same version of the same
// collections' configs.
func sameConfigs(a, b map[syntax.NSID]CollectionConfig) bool {
	return maps.EqualFunc(a, b, func(x, y CollectionConfig) bool { return x.Rev == y.Rev })
}

// reproject indexes every repo in org's spaces again from its first op, so
// its documents follow org's current configs. It runs as its own job, so the
// cursors it resets aren't then advanced by a job that was part way through.
func (x *Indexer) reproject(ctx context.Context, org syntax.DID) error {
	cursors, err := x.index.Cursors(ctx)
	if err != nil {
		return err
	}
	for _, c := range cursors {
		if c.Space.SpaceOwner() != org {
			continue
		}
		if err := x.index.SetCursor(
			ctx,
			RepoCursor{Space: c.Space, Repo: c.Repo, Rev: ""},
		); err != nil {
			return err
		}
		x.enqueue(job{kind: jobIndexRepo, space: c.Space, repo: c.Repo})
	}
	return nil
}

// queueAccessRefresh queues recomputing the Access of space and of every
// space that inherits roles from it.
func (x *Indexer) queueAccessRefresh(
	ctx context.Context,
	perms PermSource,
	space habitat_syntax.SpaceURI,
) error {
	dependents, err := perms.ListDependentSpaces(ctx, space)
	if err != nil {
		return fmt.Errorf("list dependent spaces: %w", err)
	}
	for _, s := range append([]habitat_syntax.SpaceURI{space}, dependents...) {
		x.enqueue(job{kind: jobRefreshAccess, space: s})
	}
	return nil
}

// refreshAccess recomputes space's Access and rewrites its documents with it.
func (x *Indexer) refreshAccess(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	space habitat_syntax.SpaceURI,
) error {
	access, err := spaceAccess(ctx, source, perms, space)
	if err != nil {
		return err
	}
	return x.index.SetSpaceAccess(ctx, space, access)
}

// spaceAccess resolves who may read space: anyone for an opensocial about
// space; otherwise the users the perms store resolves as readers, and the
// community roles its community.opensocial.access record names.
func spaceAccess(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	space habitat_syntax.SpaceURI,
) (Access, error) {
	if space.SpaceType() == opensocial.AboutSpaceType {
		return Access{Public: true}, nil
	}
	users, err := perms.ListUserSubjects(ctx, space, habitat_syntax.SpaceRoleReader)
	if err != nil {
		return Access{}, fmt.Errorf("list readers: %w", err)
	}
	access := Access{Principals: Principals{Users: users}}
	rec, err := source.GetRecord(ctx, space, space.SpaceOwner(), accessCollection, "self")
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return access, nil
	}
	if err != nil {
		return Access{}, fmt.Errorf("get access record: %w", err)
	}
	// Decoded through JSON, as the opensocial store does.
	raw, err := json.Marshal(rec.Value)
	if err != nil {
		return Access{}, fmt.Errorf("encode access record: %w", err)
	}
	var record opensocial_api.CommunityOpensocialAccess
	if err := json.Unmarshal(raw, &record); err != nil {
		return Access{}, fmt.Errorf("decode access record: %w", err)
	}
	for _, role := range record.Roles {
		access.CommunityRoles = append(access.CommunityRoles,
			CommunityRole{Community: space.SpaceOwner(), Role: role})
	}
	return access, nil
}

// changesAccess reports whether ops write a permission record.
func changesAccess(ops []spaces.Record) bool {
	for _, op := range ops {
		if accessCollections[op.Collection] {
			return true
		}
	}
	return false
}

// hasDocuments reports whether ops write a record that gets indexed.
func hasDocuments(ops []spaces.Record) bool {
	for _, op := range ops {
		if op.Value != nil && !habitat_syntax.ReservedCollections.Contains(op.Collection) {
			return true
		}
	}
	return false
}

// apply writes one page of ops to the index, each document readable per
// access and projected with its collection's config. A record of a
// collection with no config is deleted, so removing a config drops its
// documents. The oplog holds one op per record, its latest state, so a page
// never touches a record twice.
func (x *Indexer) apply(
	ctx context.Context,
	configs ConfigSource,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	ops []spaces.Record,
	access *Access,
) error {
	cfgs, err := x.configsFor(ctx, configs, space.SpaceOwner())
	if err != nil {
		return err
	}
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
		cfg := DefaultConfig()
		if configs != nil {
			var ok bool
			if cfg, ok = cfgs[op.Collection]; !ok {
				deleted = append(deleted, uri)
				continue
			}
		}
		d := Document{
			URI:        uri,
			Space:      space,
			Repo:       repo,
			Collection: op.Collection,
			Rev:        syntax.TID(op.Rev),
			Access:     *access,
		}
		project(&d, op.Value, cfg)
		if !cfg.NoBlobs {
			// Blob content is part of the record's document, so it keeps the
			// record's access and collection and hydrates as the record.
			d.Blobs = x.blobTexts(ctx, op.Value)
		}
		if d.Title == "" && d.Text == "" && d.Extra == "" && len(d.Blobs) == 0 {
			// Nothing to search or embed, and embeddings APIs reject empty
			// input. Deleting also drops a version of the record that had text.
			deleted = append(deleted, uri)
			continue
		}
		docs = append(docs, d)
	}
	if err := x.index.Put(ctx, docs...); err != nil {
		return err
	}
	return x.index.Delete(ctx, deleted...)
}
