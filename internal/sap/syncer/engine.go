// Package syncer keeps sap's copy of every tracked repo in sync with its host.
// Repos are discovered via Track (backfill crawl or notifications) and advance
// through a small state machine: pending/error repos are synced incrementally
// with listRepoOps and verified against the host's signed commit; repos whose
// verification fails are marked desynced and rebuilt from a full getRepo CAR
// snapshot. Synced records are handed to an Emitter (the outbox).
package syncer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

type repoState string

const (
	statePending  repoState = "pending"
	stateSyncing  repoState = "syncing"
	stateActive   repoState = "active"
	stateDesynced repoState = "desynced"
	stateError    repoState = "error"
)

// repo is the sync state of one repo within one space. Hash is the repo's
// LtHash state (spacecommit.LtHashStateBytes) as of Rev, verified against the
// host's signed commit.
type repo struct {
	Space habitat_syntax.SpaceURI `gorm:"primaryKey"`
	DID   syntax.DID              `gorm:"column:did;primaryKey"`
	Rev   syntax.TID
	State repoState `gorm:"index"`
	Hash  []byte

	// Dirty records that a notification arrived while the repo was mid-sync;
	// instead of settling active, the worker requeues it for another pass.
	Dirty bool `gorm:"not null;default:false"`

	ErrorMsg   string
	RetryCount int   `gorm:"not null;default:0"`
	RetryAfter int64 `gorm:"not null;default:0;index"`
}

func (repo) TableName() string { return "sap_repos" }

// AutoMigrate creates the syncer tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&repo{})
}

// Clients supplies an HTTP client authenticated as some session that can
// access the space. Satisfied by session.Store.
type Clients interface {
	ClientForSpace(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) (*http.Client, error)
}

// Emitter receives synced records for delivery to the consumer. Satisfied by
// an outbox adapter. InTx returns an Emitter whose writes join tx, so a batch
// of records and the repo-state advance commit atomically.
type Emitter interface {
	Emit(ctx context.Context, uri habitat_syntax.SpaceRecordURI, value []byte) error
	InTx(tx *gorm.DB) Emitter
}

type job struct {
	Space habitat_syntax.SpaceURI
	DID   syntax.DID
	// Recover routes the job to a full getRepo CAR rebuild instead of an
	// incremental listRepoOps sync.
	Recover bool
}

// Engine schedules and runs repo sync work.
type Engine struct {
	db          *gorm.DB
	clients     Clients
	emitter     Emitter
	verifier    *Verifier
	parallelism int
	notif       *utils.PollNotifier
	jobs        chan job
	metrics     *metrics
}

func New(
	db *gorm.DB,
	clients Clients,
	emitter Emitter,
	verifier *Verifier,
	parallelism int,
	m *metrics,
) *Engine {
	if parallelism <= 0 {
		parallelism = 5
	}
	return &Engine{
		db:          db,
		clients:     clients,
		emitter:     emitter,
		verifier:    verifier,
		parallelism: parallelism,
		notif:       utils.NewPollNotifier(),
		jobs:        make(chan job),
		metrics:     m,
	}
}

// WithTx returns an Engine whose database writes join tx. Scheduling state
// (notifier, workers) is shared with the original.
func (e *Engine) WithTx(tx *gorm.DB) *Engine {
	c := *e
	c.db = tx
	return &c
}

// Track starts tracking a repo, if it isn't already, and wakes the dispatcher.
func (e *Engine) Track(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
) error {
	if err := e.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(&repo{Space: space, DID: did, State: statePending}).Error; err != nil {
		return fmt.Errorf("track repo: %w", err)
	}
	e.notif.Notify()
	return nil
}

// NotifyWrite reacts to a host notification that a repo advanced: it tracks
// the repo if unknown, and requeues a settled repo when the notified rev is
// ahead of ours or the notified commit hash differs from our verified one.
func (e *Engine) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rev syntax.TID,
	hash []byte,
) error {
	err := e.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var r repo
		err := tx.Where("space = ? AND did = ?", space, did).First(&r).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return tx.Create(&repo{Space: space, DID: did, State: statePending}).Error
		}
		if err != nil {
			return err
		}
		switch r.State {
		case statePending, stateDesynced:
			// Already claimable (or headed for full recovery); nothing to do.
			return nil
		case stateSyncing:
			// Mid-flight: the in-progress pass may already be past this write.
			// Mark the repo dirty so the worker requeues instead of settling.
			return tx.Model(&repo{}).
				Where("space = ? AND did = ?", space, did).
				Update("dirty", true).Error
		}
		behind := rev != "" && r.Rev < rev
		if !behind && len(hash) > 0 {
			lt := spacecommit.Load(r.Hash)
			behind = !bytes.Equal(lt.Sum(), hash)
		}
		if !behind {
			return nil
		}
		return tx.Model(&repo{}).
			Where("space = ? AND did = ?", space, did).
			Updates(map[string]any{
				"state":       statePending,
				"retry_count": 0,
				"retry_after": 0,
			}).Error
	})
	if err != nil {
		return fmt.Errorf("notify write: %w", err)
	}
	e.notif.Notify()
	return nil
}

// DropSpace stops tracking every repo in the space.
func (e *Engine) DropSpace(ctx context.Context, space habitat_syntax.SpaceURI) error {
	return e.db.WithContext(ctx).
		Where("space = ?", space).
		Delete(&repo{}).Error
}

// Run drives the dispatcher and worker pool until ctx ends.
func (e *Engine) Run(ctx context.Context) {
	go e.runDispatcher(ctx)
	for i := 0; i < e.parallelism; i++ {
		go e.runWorker(ctx, i)
	}
	// Sweep for repos left claimable by a prior process lifetime: nothing else
	// will notify the dispatcher about them.
	e.dispatch(ctx)
	<-ctx.Done()
}

func (e *Engine) runDispatcher(ctx context.Context) {
	notify := e.notif.Listen()
	for {
		select {
		case <-ctx.Done():
			return
		case <-notify:
			e.dispatch(ctx)
		}
	}
}

func (e *Engine) dispatch(ctx context.Context) {
	ctx, span := e.metrics.tracer.Start(ctx, "sap.syncer.dispatch")
	start := time.Now()
	defer func() {
		e.metrics.dispatchFinished(ctx, start)
		span.End()
	}()

	total := 0
	// Sync pending and errored repos incrementally, prioritizing
	// freshly-discovered (pending) repos over retries (error).
	total += e.claimAndQueue(ctx, span,
		"state IN ('pending', 'error')",
		"CASE state WHEN 'pending' THEN 1 WHEN 'error' THEN 2 END",
		false)
	// Rebuild desynced repos from a full getRepo CAR snapshot.
	total += e.claimAndQueue(ctx, span, "state = 'desynced'", "space", true)
	span.SetAttributes(attribute.Int("sap.jobs_claimed", total))
}

// claimAndQueue atomically claims repos matching whereStates (a constant SQL
// predicate) into the syncing state and queues them as jobs, in batches until
// none remain. priority is a constant SQL ORDER BY expression. recover tags
// the queued jobs for getRepo recovery. Returns the number of repos claimed.
func (e *Engine) claimAndQueue(
	ctx context.Context,
	span trace.Span,
	whereStates string,
	priority string,
	recover bool,
) int {
	now := time.Now().Unix()
	claimed := 0
	query := fmt.Sprintf(`
		UPDATE sap_repos SET state = 'syncing'
		WHERE (space, did) IN (
			SELECT space, did FROM sap_repos
			WHERE (%s) AND (retry_after = 0 OR retry_after < ?)
			ORDER BY %s, space, did
			LIMIT ?
		)
		RETURNING space, did
	`, whereStates, priority)
	for {
		rows, err := e.db.WithContext(ctx).Raw(query, now, 100).Rows()
		if err != nil {
			slog.ErrorContext(ctx, "claim batch", "err", err)
			span.RecordError(err)
			return claimed
		}
		var jobs []job
		for rows.Next() {
			j := job{Recover: recover}
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
			case e.jobs <- j:
			case <-ctx.Done():
				return claimed
			}
		}
		if len(jobs) < 100 {
			return claimed
		}
	}
}

func (e *Engine) runWorker(ctx context.Context, workerID int) {
	logger := slog.Default().With("component", "syncer", "worker", workerID)
	for {
		select {
		case <-ctx.Done():
			return
		case j := <-e.jobs:
			e.runJob(ctx, logger, j)
		}
	}
}

func (e *Engine) runJob(ctx context.Context, logger *slog.Logger, j job) {
	ctx, span := e.metrics.tracer.Start(ctx, "sap.syncer.sync_repo",
		trace.WithAttributes(
			attribute.String("sap.space", j.Space.String()),
			attribute.String("sap.repo", j.DID.String()),
		))
	start := time.Now()
	e.metrics.workerBusy(ctx)
	status := "success"
	defer func() {
		e.metrics.workerIdle(ctx)
		e.metrics.jobFinished(ctx, start, status)
		span.End()
	}()

	var err error
	if j.Recover {
		err = e.recoverRepo(ctx, j.Space, j.DID)
	} else {
		err = e.syncRepo(ctx, j.Space, j.DID)
	}
	if err != nil {
		status = "error"
		span.RecordError(err)
		logger.ErrorContext(ctx, "sync failed",
			"space", j.Space, "repo", j.DID, "recover", j.Recover, "err", err)
	}
}

// settle finishes a successful sync pass, recording the repo's new rev and
// LtHash state. If a notification arrived mid-flight (dirty), the repo is
// requeued as pending for another pass instead of settling active.
func (e *Engine) settle(
	ctx context.Context,
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	rev syntax.TID,
	hashState []byte,
) error {
	updates := map[string]any{
		"state":       stateActive,
		"rev":         rev,
		"hash":        hashState,
		"error_msg":   "",
		"retry_count": 0,
		"retry_after": 0,
	}
	res := tx.WithContext(ctx).Model(&repo{}).
		Where("space = ? AND did = ? AND dirty = ?", space, did, false).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected > 0 {
		return nil
	}
	updates["state"] = statePending
	updates["dirty"] = false
	if err := tx.WithContext(ctx).Model(&repo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(updates).Error; err != nil {
		return err
	}
	e.notif.Notify()
	return nil
}

// scheduleRetry parks a repo in state with a retry backoff and records why.
// It wakes the dispatcher so the retry is picked up once due.
func (e *Engine) scheduleRetry(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	state repoState,
	cause error,
) error {
	var r repo
	err := e.db.WithContext(ctx).
		Where("space = ? AND did = ?", space, did).
		First(&r).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	retryCount := 0
	if err == nil {
		retryCount = r.RetryCount + 1
	}
	errMsg := ""
	if cause != nil {
		errMsg = cause.Error()
	}
	if err := e.db.WithContext(ctx).
		Model(&repo{}).
		Where("space = ? AND did = ?", space, did).
		Updates(map[string]any{
			"state":       state,
			"dirty":       false, // the retry re-syncs to head anyway
			"error_msg":   errMsg,
			"retry_count": retryCount,
			"retry_after": time.Now().Add(backoff(retryCount, 60)).Unix(),
		}).Error; err != nil {
		return err
	}
	e.notif.Notify()
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
