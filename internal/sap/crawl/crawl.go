// Package crawl backfills sap's view of what a session can see: for each
// session it pages through listSpaces, records space access, and enumerates
// each space's repos into the sync engine. Crawls are resumable via a stored
// cursor and re-run from where they left off after a restart.
package crawl

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

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

// crawl is the persisted progress of one session's backfill.
type crawl struct {
	DID       syntax.DID `gorm:"column:did;primaryKey"`
	State     crawlState
	Cursor    string
	ErrorMsg  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (crawl) TableName() string { return "sap_crawls" }

// AutoMigrate creates the crawl tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&crawl{})
}

// Clients supplies an HTTP client authenticated as a session. Satisfied by
// session.Store.
type Clients interface {
	ClientForSession(ctx context.Context, did syntax.DID) (*http.Client, error)
}

// Access records which spaces a session can reach. Satisfied by session.Store.
type Access interface {
	RecordSpaceAccess(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		did syntax.DID,
	) error
}

// Tracker receives discovered repos. Satisfied by syncer.Engine.
type Tracker interface {
	Track(ctx context.Context, space habitat_syntax.SpaceURI, repo syntax.DID) error
}

// Crawler runs backfill crawls.
type Crawler struct {
	db      *gorm.DB
	clients Clients
	access  Access
	tracker Tracker

	tracer          trace.Tracer
	crawlsCompleted metric.Int64Counter
	crawlDuration   metric.Float64Histogram
}

func New(
	db *gorm.DB,
	clients Clients,
	access Access,
	tracker Tracker,
	meter metric.Meter,
	tracer trace.Tracer,
) (*Crawler, error) {
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
		clients:         clients,
		access:          access,
		tracker:         tracker,
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
		go c.Run(detachCancel(ctx), cr.DID)
	}
	return nil
}

// Run crawls everything the session can see, resuming from any stored cursor.
// It is safe to re-run for the same session; discovery is idempotent.
func (c *Crawler) Run(ctx context.Context, did syntax.DID) {
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
	if err := c.db.WithContext(ctx).Model(&crawl{}).
		Where("did = ?", did).
		Updates(map[string]any{"state": stateRunning, "error_msg": ""}).Error; err != nil {
		slog.ErrorContext(ctx, "set crawl running", "session", did, "err", err)
		span.RecordError(err)
		return
	}

	if err := c.crawlSession(ctx, did, cr.Cursor); err != nil {
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

func (c *Crawler) crawlSession(ctx context.Context, did syntax.DID, cursor string) error {
	client, err := c.clients.ClientForSession(ctx, did)
	if err != nil {
		return fmt.Errorf("client for session: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		params := url.Values{}
		if cursor != "" {
			params.Set("cursor", cursor)
		}
		resp, err := client.Get("/xrpc/network.habitat.space.listSpaces?" + params.Encode())
		if err != nil {
			return fmt.Errorf("list spaces: %w", err)
		}
		var output habitat.NetworkHabitatSpaceListSpacesOutput
		decodeErr := json.NewDecoder(resp.Body).Decode(&output)
		closeErr := resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("list spaces: %s", resp.Status)
		}
		if decodeErr != nil {
			return fmt.Errorf("decode list spaces: %w", decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}

		if len(output.Spaces) == 0 {
			return nil
		}
		for _, sp := range output.Spaces {
			space := habitat_syntax.SpaceURI(sp.Uri)
			if err := c.access.RecordSpaceAccess(ctx, space, did); err != nil {
				return fmt.Errorf("record space access %s: %w", space, err)
			}
			if err := c.enumerateRepos(ctx, client, space); err != nil {
				return fmt.Errorf("enumerate repos for %s: %w", space, err)
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

func (c *Crawler) enumerateRepos(
	ctx context.Context,
	client *http.Client,
	space habitat_syntax.SpaceURI,
) error {
	params := url.Values{"space": []string{space.String()}}
	resp, err := client.Get("/xrpc/network.habitat.space.listRepos?" + params.Encode())
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()

	var output habitat.NetworkHabitatSpaceListReposOutput
	if err := json.NewDecoder(resp.Body).Decode(&output); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("list repos: %s", resp.Status)
	}

	for _, r := range output.Repos {
		if err := c.tracker.Track(ctx, space, syntax.DID(r.Did)); err != nil {
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
