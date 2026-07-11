package sap

import (
	"context"
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
	now := time.Now().Unix()
	totalClaimed := 0
	for {
		rows, err := r.db.WithContext(ctx).Raw(`
			UPDATE managed_repos SET state = 'resyncing'
			WHERE (space, did) IN (
				SELECT space, did FROM managed_repos
				WHERE state IN ('pending', 'desynced', 'error') AND (retry_after = 0 OR retry_after < ?)
				ORDER BY
					CASE state
						WHEN 'pending' THEN 1
						WHEN 'desynced' THEN 2
						WHEN 'error' THEN 3
					END,
					space, did
				LIMIT ?
			)
			RETURNING space, did
		`, now, 100).Rows()
		if err != nil {
			slog.ErrorContext(ctx, "claim batch", "err", err)
			span.RecordError(err)
			return
		}
		var jobs []resyncJob
		for rows.Next() {
			var j resyncJob
			if err := rows.Scan(&j.Space, &j.DID); err != nil {
				_ = rows.Close()
				slog.ErrorContext(ctx, "scan job", "err", err)
				span.RecordError(err)
				return
			}
			jobs = append(jobs, j)
		}
		_ = rows.Close()
		if err := rows.Err(); err != nil {
			slog.ErrorContext(ctx, "rows err", "err", err)
			span.RecordError(err)
			return
		}
		if len(jobs) == 0 {
			span.SetAttributes(attribute.Int("sap.jobs_claimed", totalClaimed))
			return
		}
		totalClaimed += len(jobs)
		for _, j := range jobs {
			select {
			case r.jobs <- j:
			case <-ctx.Done():
				span.SetAttributes(attribute.Int("sap.jobs_claimed", totalClaimed))
				return
			}
		}
		if len(jobs) < 100 {
			span.SetAttributes(attribute.Int("sap.jobs_claimed", totalClaimed))
			return
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

	if err := r.syncRepo(ctx, job.Space, job.DID); err != nil {
		status = "error"
		span.RecordError(err)
		logger.ErrorContext(
			ctx,
			"resync failed",
			"space",
			job.Space,
			"repo",
			job.DID,
			"err",
			err,
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
			r.outboxNotif.Notify()
			if output.Cursor != "" {
				since = output.Cursor
			}
		}

		if output.Cursor == "" || len(output.Ops) == 0 {
			break
		}
	}

	if err := r.db.WithContext(ctx).
		Clauses(clause.Returning{}).
		Model(&repo).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{
			"state":       RepoStateActive,
			"rev":         syntax.TID(since),
			"error_msg":   "",
			"retry_count": 0,
			"retry_after": 0,
		}).Error; err != nil {
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

func backoff(retries int, maxMinutes int) time.Duration {
	dur := 1 << retries
	if dur > maxMinutes {
		dur = maxMinutes
	}
	jitter := time.Millisecond * time.Duration(rand.Intn(1000))
	return time.Minute*time.Duration(dur) + jitter
}
