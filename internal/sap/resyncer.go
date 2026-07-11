package sap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type resyncJob struct {
	Space habitat_syntax.SpaceURI
	DID   syntax.DID
	// Recover routes the job to a full getRepo CAR recovery (for desynced repos)
	// instead of an incremental listRepoOps pull.
	Recover bool
}

// resyncer schedules resync workers to backfill repos
type resyncer struct {
	db          *gorm.DB
	oauthClient *oauthclient.App
	resyncBuf   *resyncBuffer
	parallelism int
	resyncNotif *utils.PollNotifier
	outboxNotif *utils.PollNotifier
	jobs        chan resyncJob
	metrics     *metrics
}

func newResyncer(
	db *gorm.DB,
	oauthClient *oauthclient.App,
	resyncBuf *resyncBuffer,
	resyncNotif *utils.PollNotifier,
	outboxNotif *utils.PollNotifier,
	parallelism int,
	metrics *metrics,
) *resyncer {
	if parallelism <= 0 {
		parallelism = 5
	}
	return &resyncer{
		db:          db,
		oauthClient: oauthClient,
		resyncBuf:   resyncBuf,
		parallelism: parallelism,
		resyncNotif: resyncNotif,
		outboxNotif: outboxNotif,
		jobs:        make(chan resyncJob),
		metrics:     metrics,
	}
}

func (r *resyncer) run(ctx context.Context) {
	go r.runDispatcher(ctx)
	for i := 0; i < r.parallelism; i++ {
		go r.runWorker(ctx, i)
	}
	// Sweep for repos left pending/desynced/error by a prior process
	// lifetime: nothing else will notify the dispatcher about them, since
	// notifications only fire on new discovery or new live events.
	r.dispatch(ctx)
	<-ctx.Done()
}

func (r *resyncer) runDispatcher(ctx context.Context) {
	slog.InfoContext(ctx, "resync dispatcher started")
	notify := r.resyncNotif.Listen()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			slog.InfoContext(ctx, "dispatcher received notification")
			r.dispatch(ctx)
		}
	}
}

func (r *resyncer) dispatch(ctx context.Context) {
	ctx, span := r.metrics.tracer.Start(ctx, "sap.resyncer.dispatch")
	start := time.Now()
	defer func() {
		r.metrics.dispatchFinished(ctx, start)
		span.End()
	}()

	slog.InfoContext(ctx, "resync dispatch called")
	total := 0
	// Sync pending and newly-errored repos incrementally via listRepoOps,
	// prioritizing freshly-discovered (pending) repos over retries (error).
	total += r.claimAndQueue(ctx, span,
		"state IN ('pending', 'error')",
		"CASE state WHEN 'pending' THEN 1 WHEN 'error' THEN 2 END",
		false)
	// Recover desynced repos from a full getRepo CAR snapshot.
	total += r.claimAndQueue(ctx, span, "state = 'desynced'", "space", true)
	span.SetAttributes(attribute.Int("sap.jobs_claimed", total))
}

// claimAndQueue atomically claims repos matching whereStates (a constant SQL
// predicate) into the resyncing state and queues them as jobs, in batches until
// none remain. priority is a constant SQL ORDER BY expression. recover tags the
// queued jobs for getRepo recovery. It returns the number of repos claimed.
func (r *resyncer) claimAndQueue(
	ctx context.Context,
	span trace.Span,
	whereStates string,
	priority string,
	recover bool,
) int {
	now := time.Now().Unix()
	claimed := 0
	query := fmt.Sprintf(`
		UPDATE managed_repos SET state = 'resyncing'
		WHERE (space, did) IN (
			SELECT space, did FROM managed_repos
			WHERE (%s) AND (retry_after = 0 OR retry_after < ?)
			ORDER BY %s, space, did
			LIMIT ?
		)
		RETURNING space, did
	`, whereStates, priority)
	for {
		rows, err := r.db.WithContext(ctx).Raw(query, now, 100).Rows()
		if err != nil {
			slog.ErrorContext(ctx, "claim batch", "err", err)
			span.RecordError(err)
			return claimed
		}
		var jobs []resyncJob
		for rows.Next() {
			j := resyncJob{Recover: recover}
			if err := rows.Scan(&j.Space, &j.DID); err != nil {
				_ = rows.Close()
				slog.ErrorContext(ctx, "scan job", "err", err)
				span.RecordError(err)
				return claimed
			}
			jobs = append(jobs, j)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			slog.ErrorContext(ctx, "rows err", "err", err)
			span.RecordError(err)
			return claimed
		}
		if len(jobs) == 0 {
			return claimed
		}
		claimed += len(jobs)
		for _, j := range jobs {
			select {
			case r.jobs <- j:
			case <-ctx.Done():
				return claimed
			}
		}
		if len(jobs) < 100 {
			return claimed
		}
	}
}

func (r *resyncer) runWorker(ctx context.Context, workerID int) {
	logger := slog.Default().With("component", "resyncer", "worker", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-r.jobs:
			logger.InfoContext(ctx, "resync repo", "space", job.Space, "repo", job.DID)
			r.runJob(ctx, logger, job)
		}
	}
}

func (r *resyncer) runJob(ctx context.Context, logger *slog.Logger, job resyncJob) {
	ctx, span := r.metrics.tracer.Start(ctx, "sap.resyncer.sync_repo",
		trace.WithAttributes(
			attribute.String("sap.space", job.Space.String()),
			attribute.String("sap.repo", job.DID.String()),
		))
	start := time.Now()
	r.metrics.resyncWorkerBusy(ctx)
	status := "success"
	defer func() {
		r.metrics.resyncWorkerIdle(ctx)
		r.metrics.resyncJobFinished(ctx, start, status)
		span.End()
	}()

	var syncErr error
	if job.Recover {
		syncErr = r.recoverRepo(ctx, job.Space, job.DID)
	} else {
		syncErr = r.syncRepo(ctx, job.Space, job.DID)
	}
	if syncErr != nil {
		status = "error"
		span.RecordError(syncErr)
		logger.ErrorContext(
			ctx,
			"resync failed",
			"space",
			job.Space,
			"repo",
			job.DID,
			"recover",
			job.Recover,
			"err",
			syncErr,
		)
	}
}

// clientForSpace returns an OAuth http.Client for any managed account that can
// access the space (as recorded during crawl), so the resyncer isn't tied to
// the space owner being managed. It tries each associated account until one
// yields a working client.
func (r *resyncer) clientForSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) (*http.Client, error) {
	var assocs []managedSpace
	if err := r.db.WithContext(ctx).
		Where("space = ?", space).
		Find(&assocs).Error; err != nil {
		return nil, fmt.Errorf("load managed accounts for space: %w", err)
	}

	// Candidate accounts: everyone recorded as able to access the space, plus the
	// space owner (which is often — but not always — a managed account itself).
	seen := make(map[syntax.DID]struct{})
	var candidates []syntax.DID
	for _, a := range assocs {
		if _, ok := seen[a.DID]; !ok {
			seen[a.DID] = struct{}{}
			candidates = append(candidates, a.DID)
		}
	}
	if owner := space.SpaceOwner(); owner != "" {
		if _, ok := seen[owner]; !ok {
			candidates = append(candidates, owner)
		}
	}

	var errs []error
	for _, did := range candidates {
		var org managedOrg
		if err := r.db.WithContext(ctx).First(&org, "did = ?", did).Error; err != nil {
			errs = append(errs, err)
			continue
		}
		client, err := r.oauthClient.GetClient(ctx, org.DID, org.SessionID)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		return client, nil
	}
	return nil, fmt.Errorf("no working client for space %s: %w", space, errors.Join(errs...))
}

// enqueueRepo schedules a repo for sync in response to an out-of-band trigger
// (a notifyWrite from the space host). It ensures the repo is tracked and, if an
// already-synced repo has fallen behind the notified rev, returns it to a
// claimable state, then wakes the dispatcher. rev may be empty when unknown.
func (r *resyncer) enqueueRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	rev syntax.TID,
) error {
	if err := r.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&managedRepo{Space: space, DID: repoDID, State: RepoStatePending}).Error; err != nil {
		return fmt.Errorf("track repo: %w", err)
	}
	// Re-queue an active or errored repo only when the write advances it past the
	// rev we have; leave pending/resyncing (already claimable/in-flight) and
	// desynced (headed for getRepo recovery) untouched.
	if rev != "" {
		if err := r.db.WithContext(ctx).
			Model(&managedRepo{}).
			Where("space = ? AND did = ? AND state IN ? AND rev < ?",
				space, repoDID, []repoState{RepoStateActive, RepoStateError}, rev).
			Update("state", RepoStatePending).Error; err != nil {
			return fmt.Errorf("requeue repo: %w", err)
		}
	}
	r.resyncNotif.Notify()
	return nil
}

func (r *resyncer) syncRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) error {
	var repo managedRepo
	err := r.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, repoDID).
		First(&repo).Error
	if err != nil {
		return err
	}
	since := ""
	if repo.Rev != "" {
		since = repo.Rev.String()
	}

	// A full pull (no cursor) sees every current record exactly once, so we can
	// fold the repo's LtHash from scratch and check it against the signed commit
	// at head. Incremental pulls can't reconstruct the set (deletes aren't
	// represented in listRepoOps), so they skip verification.
	fullPull := since == ""
	var lt spacecommit.LtHash
	var headCommit habitat.NetworkHabitatSpaceDefsSignedCommit

	client, err := r.clientForSpace(ctx, space)
	if err != nil {
		return fmt.Errorf("get client for space: %w", err)
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{
			"space": []string{space.String()},
			"repo":  []string{repoDID.String()},
		}
		if since != "" {
			params.Set("since", since)
		}
		params.Set("limit", "1000")

		req, err := http.NewRequestWithContext(ctx, "GET",
			"/xrpc/network.habitat.space.listRepoOps?"+params.Encode(), nil)
		if err != nil {
			return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("create request: %w", err))
		}
		resp, err := client.Do(req)
		if err != nil {
			return r.handleSyncError(ctx, space, repoDID, err)
		}

		var output habitat.NetworkHabitatSpaceListRepoOpsOutput
		decodeErr := json.NewDecoder(resp.Body).Decode(&output)
		closeErr := resp.Body.Close()
		if decodeErr != nil {
			return r.handleSyncError(ctx, space, repoDID, decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if resp.StatusCode != http.StatusOK {
			slog.WarnContext(
				ctx,
				"listRepoOps status",
				"space",
				space,
				"repo",
				repoDID,
				"status",
				resp.StatusCode,
			)
			return r.handleSyncError(
				ctx,
				space,
				repoDID,
				fmt.Errorf("listRepoOps: %s", resp.Status),
			)
		}

		slog.InfoContext(
			ctx,
			"listRepoOps response",
			"space",
			space,
			"repo",
			repoDID,
			"ops",
			len(output.Ops),
			"cursor",
			output.Cursor,
		)

		if len(output.Ops) > 0 {
			err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
				if err := writeRepoOps(tx, space, repoDID, output.Ops); err != nil {
					return err
				}
				lastRev := syntax.TID(output.Ops[len(output.Ops)-1].Rev)
				return tx.Model(&managedRepo{}).
					Where("space = ? AND did = ?", space, repoDID).
					Update("rev", lastRev).Error
			})
			if err != nil {
				return r.handleSyncError(ctx, space, repoDID, err)
			}
			if err := foldOps(&lt, output.Ops); err != nil {
				return r.handleSyncError(ctx, space, repoDID, err)
			}
			r.outboxNotif.Notify()
			if output.Cursor != "" {
				since = output.Cursor
			}
		}

		// The signed commit is only present once the head of the oplog is
		// reached; keep the latest one so we can verify after the loop.
		headCommit = output.Commit

		if output.Cursor == "" || len(output.Ops) == 0 {
			break
		}
	}

	verifiedHash, proceed, err := r.verifyHead(ctx, space, repoDID, fullPull, &lt, headCommit)
	if err != nil {
		return err
	}
	if !proceed {
		// Hash mismatch: the repo was marked desynced for a fresh full pull.
		return nil
	}

	updates := map[string]any{
		"state":       RepoStateActive,
		"rev":         syntax.TID(since),
		"error_msg":   "",
		"retry_count": 0,
		"retry_after": 0,
	}
	// Only overwrite the stored hash when this full pull verified one; an
	// incremental pull must not clobber the last-verified hash with nil.
	if verifiedHash != nil {
		updates["hash"] = verifiedHash
	}
	if err := r.db.WithContext(ctx).
		Clauses(clause.Returning{}).
		Model(&repo).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(updates).Error; err != nil {
		return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("set active: %w", err))
	}
	if err := r.resyncBuf.drainRepo(ctx, &repo); err != nil {
		if markErr := r.db.WithContext(ctx).
			Model(&managedRepo{}).
			Where("space = ? AND did = ?", space, repoDID).
			Update("state", RepoStateDesynced).Error; markErr != nil {
			return errors.Join(err, markErr)
		}
		return fmt.Errorf("drain repo after sync: %w", err)
	}
	return nil
}

// recoverRepo rebuilds a desynced repo from a full com.atproto.space.getRepo
// CAR snapshot: it fetches the CAR, recomputes the repo's LtHash from the
// recovered records, verifies it against the CAR's signed commit, then emits the
// records to the outbox and marks the repo active in a single transaction. This
// is the canonical recovery path for repos that fell out of sync (a rev-chain
// gap or a hash mismatch during an incremental pull).
func (r *resyncer) recoverRepo(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
) error {
	client, err := r.clientForSpace(ctx, space)
	if err != nil {
		return fmt.Errorf("get client for space: %w", err)
	}

	params := url.Values{
		"space": []string{space.String()},
		"repo":  []string{repoDID.String()},
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		"/xrpc/com.atproto.space.getRepo?"+params.Encode(), nil)
	if err != nil {
		return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("create request: %w", err))
	}
	resp, err := client.Do(req)
	if err != nil {
		return r.handleSyncError(ctx, space, repoDID, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("getRepo: %s", resp.Status))
	}

	recovered, err := parseRepoCAR(resp.Body)
	if err != nil {
		return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("parse repo car: %w", err))
	}

	// Recompute the LtHash from the recovered record set and verify it against
	// the signed commit carried in the CAR.
	var lt spacecommit.LtHash
	for _, rec := range recovered.Records {
		lt.Add(spacecommit.RecordElement(rec.Collection, rec.Rkey, rec.Cid.String()))
	}
	if !bytes.Equal(lt.Sum(), recovered.Commit.Hash) {
		r.metrics.repoVerified(ctx, "mismatch")
		return r.handleSyncError(ctx, space, repoDID,
			errors.New("recovered repo hash mismatch against signed commit"))
	}
	r.metrics.repoVerified(ctx, "verified")

	// Emit the recovered records and mark the repo active atomically.
	hashState := lt.State()
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for _, rec := range recovered.Records {
			value, err := json.Marshal(rec.Value)
			if err != nil {
				return fmt.Errorf("marshal record %s/%s: %w", rec.Collection, rec.Rkey, err)
			}
			uri := habitat_syntax.ConstructSpaceRecordURI(space, repoDID, rec.Collection, rec.Rkey)
			if err := tx.Create(&outbox{URI: uri, Value: value}).Error; err != nil {
				return err
			}
		}
		return tx.Model(&managedRepo{}).
			Where("space = ? AND did = ?", space, repoDID).
			Updates(map[string]any{
				"state":       RepoStateActive,
				"rev":         syntax.TID(recovered.Commit.Rev),
				"hash":        hashState,
				"error_msg":   "",
				"retry_count": 0,
				"retry_after": 0,
			}).Error
	})
	if err != nil {
		return r.handleSyncError(ctx, space, repoDID, fmt.Errorf("apply recovered repo: %w", err))
	}
	r.outboxNotif.Notify()

	// Drain any events buffered while the repo was being recovered.
	var repo managedRepo
	if err := r.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, repoDID).
		First(&repo).Error; err != nil {
		return err
	}
	if err := r.resyncBuf.drainRepo(ctx, &repo); err != nil {
		if markErr := r.db.WithContext(ctx).
			Model(&managedRepo{}).
			Where("space = ? AND did = ?", space, repoDID).
			Update("state", RepoStateDesynced).Error; markErr != nil {
			return errors.Join(err, markErr)
		}
		return fmt.Errorf("drain repo after recovery: %w", err)
	}
	return nil
}

func (r *resyncer) handleSyncError(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	syncErr error,
) error {
	var repo managedRepo
	err := r.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		First(&repo).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	retryCount := 0
	if err == nil {
		retryCount = repo.RetryCount + 1
	}
	retryAfter := time.Now().Add(backoff(retryCount, 60))
	errMsg := ""
	if syncErr != nil {
		errMsg = syncErr.Error()
	}
	return r.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(map[string]any{
			"state":       RepoStateError,
			"error_msg":   errMsg,
			"retry_count": retryCount,
			"retry_after": retryAfter.Unix(),
		}).Error
}

// verifyHead checks the repo's folded LtHash against the signed commit at the
// head of a full pull. It returns the verified 2048-byte LtHash state (nil when
// verification was skipped) and whether the caller should proceed to mark the
// repo active. On mismatch it marks the repo desynced for a fresh full pull and
// returns proceed=false.
func (r *resyncer) verifyHead(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repoDID syntax.DID,
	fullPull bool,
	lt *spacecommit.LtHash,
	commit habitat.NetworkHabitatSpaceDefsSignedCommit,
) (verifiedHash []byte, proceed bool, err error) {
	// Verification only applies to a full pull that reached a signed head
	// (commit omitted → Ver 0, e.g. an empty repo or no signer for the owner).
	if !fullPull || commit.Ver == 0 {
		return nil, true, nil
	}
	wantHash, err := decodeBytesField(commit.Hash)
	if err != nil {
		return nil, false, r.handleSyncError(
			ctx, space, repoDID, fmt.Errorf("decode commit hash: %w", err))
	}
	if !bytes.Equal(lt.Sum(), wantHash) {
		r.metrics.repoVerified(ctx, "mismatch")
		slog.WarnContext(ctx, "repo hash mismatch against signed commit; marking desynced",
			"space", space, "repo", repoDID, "rev", commit.Rev)
		if mErr := r.markHashMismatch(ctx, space, repoDID); mErr != nil {
			return nil, false, mErr
		}
		return nil, false, nil
	}
	r.metrics.repoVerified(ctx, "verified")
	return lt.State(), true, nil
}

// foldOps folds a page of listRepoOps entries into the running LtHash. Each op
// is a current record, so it folds in as a single Add.
func foldOps(lt *spacecommit.LtHash, ops []habitat.NetworkHabitatSpaceListRepoOpsOpEntry) error {
	for _, op := range ops {
		collection, err := syntax.ParseNSID(op.Collection)
		if err != nil {
			return fmt.Errorf("parse collection %q: %w", op.Collection, err)
		}
		rkey, err := syntax.ParseRecordKey(op.Rkey)
		if err != nil {
			return fmt.Errorf("parse rkey %q: %w", op.Rkey, err)
		}
		lt.Add(spacecommit.RecordElement(collection, rkey, op.Cid))
	}
	return nil
}

// decodeBytesField decodes a lexicon bytes field, which JSON-decodes into a
// base64 (std) string, into raw bytes.
func decodeBytesField(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("expected base64 string, got %T", v)
	}
	return base64.StdEncoding.DecodeString(s)
}

// markHashMismatch resets a repo whose synced state failed hash verification:
// it drops the synced rev and hash and marks it desynced so the resyncer
// re-pulls from scratch, throttled by a retry backoff to avoid hot-looping on a
// persistently divergent repo.
func (r *resyncer) markHashMismatch(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	var repo managedRepo
	err := r.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		First(&repo).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	retryCount := 0
	if err == nil {
		retryCount = repo.RetryCount + 1
	}
	retryAfter := time.Now().Add(backoff(retryCount, 60))
	if err := r.db.WithContext(ctx).
		Model(&managedRepo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(map[string]any{
			"state":       RepoStateDesynced,
			"rev":         syntax.TID(""),
			"hash":        nil,
			"error_msg":   "repo hash mismatch against signed commit",
			"retry_count": retryCount,
			"retry_after": retryAfter.Unix(),
		}).Error; err != nil {
		return err
	}
	r.resyncNotif.Notify()
	return nil
}

func backoff(retries int, maxMinutes int) time.Duration {
	dur := 1 << retries
	if dur > maxMinutes {
		dur = maxMinutes
	}
	jitter := time.Millisecond * time.Duration(rand.Intn(1000))
	return time.Minute*time.Duration(dur) + jitter
}
