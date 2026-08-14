// Package crawl backfills sap's view of what a session can see: for each
// session it pages through listSpaces (authenticated as the session),
// records space access, and enumerates each space's repos via listRepos
// (authenticated with a space credential, since listRepos spans every
// member's repos in the space) into the sync engine. Crawls are resumable
// via a stored cursor and re-run from where they left off after a restart.
package crawl

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type crawlState string

const (
	stateRunning  crawlState = "running"
	stateComplete crawlState = "complete"
	stateErrored  crawlState = "errored"
)

// crawl is the persisted progress of one session's backfill, resumable via
// SessionID so a restart after a crash can re-run Run with the same session.
type crawl struct {
	DID       syntax.DID `gorm:"column:did;primaryKey"`
	SessionID string
	State     crawlState
	Cursor    string
	ErrorMsg  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Access records which spaces a session can reach. Satisfied by session.Store.
type Access interface {
	RecordSpaceAccess(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		did syntax.DID,
		sessionID string,
	) error
}

// SpaceClients supplies a client authorized to read a space (a space
// credential). listRepos spans every member's repos in the space, so per the
// permissioned-data proposal it requires space-level authorization rather
// than the crawling session's own access token. Satisfied by
// credential.Manager, which resolves a delegating session itself from
// recorded space access (session.Store.DelegationToken).
type SpaceClients interface {
	ClientForSpace(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
	) (*atclient.APIClient, error)
}

// Tracker receives discovered repos and reports host-observed rev/hash for
// drift detection. Satisfied by syncer.Engine.
type Tracker interface {
	Track(ctx context.Context, space habitat_syntax.SpaceURI, repo syntax.DID) error
	Check(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		rev syntax.TID,
		hashB64 string,
	) error
}

// Notify subscribes sap to a discovered space's push notifications. Satisfied
// by register.Registrar; may be nil when notification registration is
// disabled.
type Notify interface {
	EnsureRegistered(ctx context.Context, space habitat_syntax.SpaceURI) error
}

// Crawler runs backfill crawls.
type Crawler struct {
	db           *gorm.DB
	oauthClient  *oauth.ClientApp
	access       Access
	spaceClients SpaceClients
	tracker      Tracker
	notify       Notify // may be nil

	// inFlight dedupes concurrent Runs for the same session within this
	// process (e.g. a periodic re-crawl overlapping a still-running crawl).
	mu       sync.Mutex
	inFlight map[syntax.DID]struct{}

	tracer          trace.Tracer
	crawlsCompleted metric.Int64Counter
	crawlDuration   metric.Float64Histogram
}

func New(
	db *gorm.DB,
	oauthClient *oauth.ClientApp,
	access Access,
	spaceClients SpaceClients,
	tracker Tracker,
	notify Notify,
	meter metric.Meter,
	tracer trace.Tracer,
) (*Crawler, error) {
	if err := db.AutoMigrate(&crawl{}); err != nil {
		return nil, err
	}
	if meter == nil {
		meter = metricnoop.NewMeterProvider().Meter("sap")
	}
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("sap")
	}
	crawlsCompleted, err := meter.Int64Counter(
		"sap.crawler.completed",
		metric.WithUnit("item"),
		metric.WithDescription("number of session crawls completed, by status"),
	)
	if err != nil {
		return nil, err
	}
	crawlDuration, err := meter.Float64Histogram(
		"sap.crawler.duration",
		metric.WithUnit("s"),
		metric.WithDescription("duration of a full session crawl"),
	)
	if err != nil {
		return nil, err
	}
	return &Crawler{
		db:              db,
		oauthClient:     oauthClient,
		access:          access,
		spaceClients:    spaceClients,
		tracker:         tracker,
		notify:          notify,
		inFlight:        make(map[syntax.DID]struct{}),
		tracer:          tracer,
		crawlsCompleted: crawlsCompleted,
		crawlDuration:   crawlDuration,
	}, nil
}

// ResumeIncomplete restarts crawls that never completed (crashed mid-run or
// were never started), each in its own goroutine.
func (c *Crawler) ResumeIncomplete(ctx context.Context) error {
	var crawls []crawl
	if err := c.db.WithContext(ctx).
		Where("state = ?", stateRunning).
		Find(&crawls).Error; err != nil {
		return fmt.Errorf("find incomplete crawls: %w", err)
	}
	for _, cr := range crawls {
		go c.Run(detachCancel(ctx), cr.DID, cr.SessionID)
	}
	return nil
}

// Run crawls everything the session can see, resuming from any stored cursor.
// A completed crawl re-runs from the beginning, so periodic re-crawls discover
// spaces created since. It is safe to re-run for the same session; discovery
// is idempotent, and overlapping Runs for one session no-op.
func (c *Crawler) Run(ctx context.Context, did syntax.DID, sessionID string) {
	c.mu.Lock()
	if _, running := c.inFlight[did]; running {
		c.mu.Unlock()
		return
	}
	c.inFlight[did] = struct{}{}
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.inFlight, did)
		c.mu.Unlock()
	}()

	ctx, span := c.tracer.Start(ctx, "sap.crawler.crawl",
		trace.WithAttributes(attribute.String("sap.session", did.String())))
	start := time.Now()
	status := "error"
	defer func() {
		c.crawlDuration.Record(ctx, time.Since(start).Seconds())
		c.crawlsCompleted.Add(ctx, 1, metric.WithAttributeSet(
			attribute.NewSet(attribute.String("status", status)),
		))
		span.End()
	}()

	var cr crawl
	if err := c.db.WithContext(ctx).
		Where(crawl{DID: did}).
		Attrs(crawl{State: stateRunning}).
		FirstOrCreate(&cr).Error; err != nil {
		slog.ErrorContext(ctx, "load crawl state", "session", did, "err", err)
		span.RecordError(err)
		return
	}
	// A finished crawl restarts from the top; an interrupted one resumes from
	// its cursor.
	if cr.State == stateComplete {
		cr.Cursor = ""
	}
	if err := c.db.WithContext(ctx).Model(&crawl{}).
		Where("did = ?", did).
		Updates(map[string]any{
			"session_id": sessionID,
			"state":      stateRunning,
			"cursor":     cr.Cursor,
			"error_msg":  "",
		}).Error; err != nil {
		slog.ErrorContext(ctx, "set crawl running", "session", did, "err", err)
		span.RecordError(err)
		return
	}

	if err := c.crawlSession(ctx, did, sessionID, cr.Cursor); err != nil {
		span.RecordError(err)
		if uErr := c.db.WithContext(ctx).Model(&crawl{}).
			Where("did = ?", did).
			Updates(map[string]any{"state": stateErrored, "error_msg": err.Error()}).
			Error; uErr != nil {
			slog.ErrorContext(ctx, "set crawl errored", "session", did, "err", uErr)
		}
		slog.ErrorContext(ctx, "crawl failed", "session", did, "err", err)
		return
	}

	if err := c.db.WithContext(ctx).Model(&crawl{}).
		Where("did = ?", did).
		Update("state", stateComplete).Error; err != nil {
		slog.ErrorContext(ctx, "set crawl complete", "session", did, "err", err)
		span.RecordError(err)
		return
	}
	status = "success"
	slog.InfoContext(ctx, "crawl finished", "session", did)
}

func (c *Crawler) crawlSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
	cursor string,
) error {
	sess, err := c.oauthClient.ResumeSession(ctx, did, sessionID)
	if err != nil {
		return fmt.Errorf("resume session %s for %s: %w", sessionID, did, err)
	}
	client := sess.APIClient()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := map[string]any{}
		if cursor != "" {
			params["cursor"] = cursor
		}
		var output habitat.NetworkHabitatSpaceListSpacesOutput
		if err := client.Get(ctx, "network.habitat.space.listSpaces", params, &output); err != nil {
			return fmt.Errorf("list spaces: %w", err)
		}

		if len(output.Spaces) == 0 {
			return nil
		}
		for _, sp := range output.Spaces {
			space := habitat_syntax.SpaceURI(sp.Uri)
			if err := c.TrackSpace(ctx, space, did, sessionID); err != nil {
				return err
			}
		}

		if output.Cursor == "" {
			return nil
		}
		cursor = output.Cursor
		if err := c.db.WithContext(ctx).Model(&crawl{}).
			Where("did = ?", did).
			Update("cursor", cursor).Error; err != nil {
			return fmt.Errorf("save crawl cursor: %w", err)
		}
	}
}

// TrackSpace records that (did, sessionID) can access space and syncs the
// space's repos, exactly as if a crawl's listSpaces had just returned it. It
// is the entry point for a space the caller already knows about — say, one
// named by an out-of-band notification or an invite — without waiting for the
// session's next crawl.
func (c *Crawler) TrackSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	sessionID string,
) error {
	if err := c.access.RecordSpaceAccess(ctx, space, did, sessionID); err != nil {
		return fmt.Errorf("record space access %s: %w", space, err)
	}
	// Subscribe to the space's push notifications as soon as we know about
	// it. Best-effort: the registrar's sweep retries misses.
	if c.notify != nil {
		if err := c.notify.EnsureRegistered(ctx, space); err != nil {
			slog.WarnContext(ctx, "register notify for space",
				"space", space, "err", err)
		}
	}
	if err := c.enumerateRepos(ctx, space); err != nil {
		return fmt.Errorf("enumerate repos for %s: %w", space, err)
	}
	return nil
}

func (c *Crawler) enumerateRepos(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) error {
	client, err := c.spaceClients.ClientForSpace(ctx, space)
	if err != nil {
		return fmt.Errorf("client for space %s: %w", space, err)
	}
	var output habitat.NetworkHabitatSpaceListReposOutput
	if err := client.Get(ctx, "network.habitat.space.listRepos",
		map[string]any{"space": space.String()}, &output); err != nil {
		return fmt.Errorf("list repos: %w", err)
	}

	for _, r := range output.Repos {
		did := syntax.DID(r.Did)
		hashB64 := ""
		if h, ok := r.Hash.(string); ok {
			hashB64 = h
		}
		if err := c.tracker.Check(ctx, space, did, syntax.TID(r.Rev), hashB64); err != nil {
			return err
		}
	}
	return nil
}

// detachCancel returns a context that keeps ctx's cancellation but starts a
// fresh trace scope, so resumed crawls don't attach to the short-lived resume
// span.
func detachCancel(ctx context.Context) context.Context {
	return trace.ContextWithRemoteSpanContext(
		trace.ContextWithSpan(ctx, tracenoop.Span{}),
		trace.SpanContextFromContext(ctx),
	)
}
