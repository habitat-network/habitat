package search

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
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
	// maxJobLinks caps how many queueing spans one job links to, when many
	// notifications collapse into it.
	maxJobLinks = 32
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
//
// Each job runs in its own span, linked to the spans that queued it, such as
// the store write that notified it. A job queued by a single write is also a
// child of that write's span, so a request's trace continues to its records
// being indexed; one that several writes collapsed into starts its own trace.
type Indexer struct {
	index Index
	// blobs, when set, supplies the content of blobs records reference; see
	// [WithBlobs].
	blobs BlobSource

	mu      sync.Mutex
	order   []job
	pending map[job]*jobState
	// running counts jobs popped from the queue that haven't finished.
	running int
	wake    chan struct{}
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
)

// job is a unit of indexing work.
type job struct {
	kind  jobKind
	space habitat_syntax.SpaceURI
	// repo is only set for jobIndexRepo.
	repo syntax.DID
}

// String names the kind, as recorded on spans and metrics.
func (k jobKind) String() string {
	switch k {
	case jobIndexRepo:
		return "index_repo"
	case jobRefreshAccess:
		return "refresh_access"
	case jobDeleteSpace:
		return "delete_space"
	}
	return "unknown"
}

// attributes are j's span attributes.
func (j job) attributes() []attribute.KeyValue {
	attrs := []attribute.KeyValue{jobKindKey.String(j.kind.String()), telemetry.Space(j.space)}
	if j.repo != "" {
		attrs = append(attrs, telemetry.Repo(j.repo))
	}
	return attrs
}

// jobState is what the queue keeps about a pending job.
type jobState struct {
	// queued is when the job was first queued.
	queued time.Time
	// links are the spans that queued the job, up to [maxJobLinks].
	links []trace.Link
}

// IndexerOption configures an [Indexer].
type IndexerOption func(*Indexer)

// NewIndexer returns an Indexer that writes to index. Nothing is indexed
// until [Indexer.Run] is called.
func NewIndexer(index Index, opts ...IndexerOption) *Indexer {
	x := &Indexer{
		index:   index,
		pending: map[job]*jobState{},
		wake:    make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(x)
	}
	return x
}

// NotifyWrite implements [spaces.Notifier] by queueing repo to be indexed.
func (x *Indexer) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	_ syntax.TID,
	_ []byte,
	_ syntax.TID,
	_ syntax.TID,
) {
	x.enqueue(ctx, job{kind: jobIndexRepo, space: space, repo: repo})
}

// NotifySpaceDeleted implements [spaces.Notifier] by queueing space's
// documents to be deleted.
func (x *Indexer) NotifySpaceDeleted(ctx context.Context, space habitat_syntax.SpaceURI) {
	x.enqueue(ctx, job{kind: jobDeleteSpace, space: space})
}

// enqueue queues j, linking it to the span in ctx, which queued it.
func (x *Indexer) enqueue(ctx context.Context, j job) {
	sc := trace.SpanContextFromContext(ctx)
	x.mu.Lock()
	st, collapsed := x.pending[j]
	if !collapsed {
		st = &jobState{queued: time.Now()}
		x.pending[j] = st
		x.order = append(x.order, j)
	}
	if sc.IsValid() && len(st.links) < maxJobLinks {
		st.links = append(st.links, trace.Link{SpanContext: sc})
	}
	x.mu.Unlock()
	// Mark on the queueing span that indexing follows, and whether this
	// joined a job already queued, whose span then links back here.
	trace.SpanFromContext(ctx).AddEvent("search.index.queued", trace.WithAttributes(
		append(j.attributes(), attribute.Bool("habitat.search.job.collapsed", collapsed))...,
	))
	select {
	case x.wake <- struct{}{}:
	default:
	}
}

// next pops the oldest pending job. A notification that arrives while the
// job runs queues it again, so the repo's new ops are picked up after.
func (x *Indexer) next() (job, *jobState, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if len(x.order) == 0 {
		return job{}, nil, false
	}
	j := x.order[0]
	x.order = x.order[1:]
	st := x.pending[j]
	delete(x.pending, j)
	x.running++
	return j, st, true
}

// done marks a job popped by next as finished.
func (x *Indexer) done() {
	x.mu.Lock()
	x.running--
	x.mu.Unlock()
}

// idle reports whether no job is queued or running.
func (x *Indexer) idle() bool {
	return x.backlog() == 0
}

// backlog counts the jobs queued or running.
func (x *Indexer) backlog() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return len(x.order) + x.running
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
		x.enqueue(ctx, j)
	}
	for _, space := range deleted {
		x.enqueue(ctx, job{kind: jobDeleteSpace, space: space})
	}
	// The queue may have filled while the index was compared, but then the
	// next check sees it.
	return len(stale) == 0 && len(deleted) == 0 && x.idle(), nil
}

// Run indexes queued repos, reading records from source and permissions from
// perms, until ctx is done. It sweeps on start and then every five minutes.
// Indexing errors are logged and retried on the next notification or sweep,
// so Run only returns ctx's error.
func (x *Indexer) Run(ctx context.Context, source RepoSource, perms PermSource) error {
	m := getMetrics()
	reg, err := m.meter.RegisterCallback(func(_ context.Context, o metric.Observer) error {
		o.ObserveInt64(m.queueSize, int64(x.backlog()))
		return nil
	}, m.queueSize)
	if err != nil {
		slog.WarnContext(ctx, "search: register indexer queue gauge failed", "err", err)
	} else {
		defer func() { _ = reg.Unregister() }()
	}
	ticker := time.NewTicker(sweepInterval)
	defer ticker.Stop()
	x.sweep(ctx, source)
	for {
		x.drain(ctx, source, perms)
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
func (x *Indexer) drain(ctx context.Context, source RepoSource, perms PermSource) {
	for ctx.Err() == nil {
		j, st, ok := x.next()
		if !ok {
			return
		}
		x.runJob(ctx, source, perms, j, st)
	}
}

// runJob runs j in its own span, following the spans that queued it, and
// logs its error, which the next notification or sweep retries.
func (x *Indexer) runJob(
	ctx context.Context,
	source RepoSource,
	perms PermSource,
	j job,
	st *jobState,
) {
	m := getMetrics()
	kind := metric.WithAttributes(jobKindKey.String(j.kind.String()))
	wait := time.Since(st.queued)
	m.queueWait.Record(ctx, wait.Seconds(), kind)

	opts := []trace.SpanStartOption{
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithLinks(st.links...),
		trace.WithAttributes(append(j.attributes(),
			attribute.Float64("habitat.search.queue_wait_s", wait.Seconds()),
			attribute.Int("habitat.search.job.queued_by", len(st.links)),
		)...),
	}
	if len(st.links) == 1 {
		// Continue the trace of the one write that queued the job.
		ctx = trace.ContextWithSpanContext(ctx, st.links[0].SpanContext)
	} else {
		opts = append(opts, trace.WithNewRoot())
	}
	ctx, span := tracer.Start(ctx, "search.indexer "+j.kind.String(), opts...)
	defer span.End()

	start := time.Now()
	var err error
	switch j.kind {
	case jobIndexRepo:
		err = x.indexRepo(ctx, source, perms, j.space, j.repo)
	case jobRefreshAccess:
		err = x.refreshAccess(ctx, source, perms, j.space)
	case jobDeleteSpace:
		err = x.index.DeleteSpace(ctx, j.space)
	}
	x.done()
	m.jobDuration.Record(ctx, secondsSince(start),
		metric.WithAttributes(jobKindKey.String(j.kind.String()), outcome(err)))
	if err != nil {
		telemetry.RecordError(span, err)
		slog.ErrorContext(ctx, "search indexing failed",
			"job", j.kind.String(), "space", j.space, "repo", j.repo,
			"queue_wait", wait, "err", err)
	}
}

// sweep queues every repo the index is behind on, and the deletion of every
// indexed space that no longer exists. It runs in a trace of its own, which
// the jobs it queues link to.
func (x *Indexer) sweep(ctx context.Context, source RepoSource) {
	ctx, span := tracer.Start(ctx, "search.indexer sweep", trace.WithNewRoot())
	defer span.End()
	if err := x.queueStale(ctx, source); err != nil {
		telemetry.RecordError(span, err)
		slog.ErrorContext(ctx, "search index sweep failed", "err", err)
	}
}

func (x *Indexer) queueStale(ctx context.Context, source RepoSource) error {
	stale, deleted, err := x.staleWork(ctx, source)
	if err != nil {
		return err
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.Int("habitat.search.stale_repos", len(stale)),
		attribute.Int("habitat.search.deleted_spaces", len(deleted)),
	)
	if len(stale) > 0 || len(deleted) > 0 {
		slog.InfoContext(ctx, "search: sweep queued work the index is behind on",
			"stale_repos", len(stale), "deleted_spaces", len(deleted))
	}
	for _, j := range stale {
		x.enqueue(ctx, j)
	}
	for _, space := range deleted {
		x.enqueue(ctx, job{kind: jobDeleteSpace, space: space})
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
		if err := x.apply(ctx, space, repo, ops, access); err != nil {
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
		if len(ops) < opsPageSize {
			return nil
		}
	}
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
		x.enqueue(ctx, job{kind: jobRefreshAccess, space: s})
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
// access. The oplog holds one op per record, its latest state, so a page
// never touches a record twice.
func (x *Indexer) apply(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	ops []spaces.Record,
	access *Access,
) error {
	var docs []Document
	var deleted []habitat_syntax.SpaceRecordURI
	// revs are the revisions written, by operation, for the lag metric.
	revs := map[string][]string{}
	for _, op := range ops {
		if habitat_syntax.ReservedCollections.Contains(op.Collection) {
			continue
		}
		uri := habitat_syntax.ConstructSpaceRecordURI(space, repo, op.Collection, op.Rkey)
		doc, ok := x.document(ctx, uri, op, access)
		if !ok {
			deleted = append(deleted, uri)
			revs["delete"] = append(revs["delete"], op.Rev)
			continue
		}
		docs = append(docs, doc)
		revs["put"] = append(revs["put"], op.Rev)
	}
	if err := x.index.Put(ctx, docs...); err != nil {
		return err
	}
	if err := x.index.Delete(ctx, deleted...); err != nil {
		return err
	}
	m := getMetrics()
	for op, rs := range revs {
		set := metric.WithAttributes(operationKey.String(op))
		m.records.Add(ctx, int64(len(rs)), set)
		for _, rev := range rs {
			// A record is searchable once its page is written, so its lag is
			// from its revision's timestamp to now.
			if tid, err := syntax.ParseTID(rev); err == nil {
				m.indexLag.Record(ctx, secondsSince(tid.Time()), set)
			}
		}
	}
	slog.DebugContext(ctx, "search: indexed repo page",
		"space", space, "repo", repo, "put", len(docs), "deleted", len(deleted),
		"rev", ops[len(ops)-1].Rev)
	return nil
}

// document returns the document of op, the record at uri, in its own span
// so the record's indexing can be found by its URI. It reports false when
// the record is to be deleted from the index instead.
func (x *Indexer) document(
	ctx context.Context,
	uri habitat_syntax.SpaceRecordURI,
	op spaces.Record,
	access *Access,
) (Document, bool) {
	ctx, span := tracer.Start(ctx, "search.indexer document", trace.WithAttributes(
		append(telemetry.Record(uri), telemetry.Rev(op.Rev))...,
	))
	defer span.End()
	if op.Value == nil {
		// A tombstone: the record was deleted.
		span.SetAttributes(operationKey.String("delete"))
		return Document{}, false
	}
	text := ExtractText(op.Value)
	// Blob content is part of the record's document, so it keeps the
	// record's access and collection and hydrates as the record.
	blobs := x.blobTexts(ctx, op.Value)
	span.SetAttributes(
		attribute.Int("habitat.search.text_bytes", len(text)),
		attribute.Int("habitat.search.blobs", len(blobs)),
	)
	if text == "" && len(blobs) == 0 {
		// Nothing to search or embed, and embeddings APIs reject empty
		// input. Deleting also drops a version of the record that had text.
		span.SetAttributes(operationKey.String("delete"))
		return Document{}, false
	}
	span.SetAttributes(operationKey.String("put"))
	repo := uri.Repo()
	return Document{
		URI:        uri,
		Space:      uri.SpaceURI(),
		Repo:       repo,
		Collection: op.Collection,
		Rev:        syntax.TID(op.Rev),
		Text:       text,
		Blobs:      blobs,
		// Collections will choose these through their search config; until
		// then a record is about its owner, and dated by its own fields.
		Users:  []syntax.DID{repo},
		Date:   recordDate(op.Value),
		Access: *access,
	}, true
}
