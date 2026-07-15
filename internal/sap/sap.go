// Package sap is a syncing service for habitat permissioned spaces, usable as
// a library. A caller adds an OAuth session after completing the auth flow;
// sap then backfills everything the session can see (listSpaces/listRepos),
// keeps notify registrations with the spaces' hosts fresh, and — when the
// caller relays a host notification via NotifyWrite / NotifySpaceDeleted —
// incrementally syncs the affected repo with listRepoOps, verifying each
// repo's LtHash against the host's signed commit. Repos that fail verification
// are rebuilt from a full getRepo snapshot. Synced records are delivered
// through an acknowledged outbox.
package sap

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/sap/crawl"
	"github.com/habitat-network/habitat/internal/sap/outbox"
	"github.com/habitat-network/habitat/internal/sap/register"
	"github.com/habitat-network/habitat/internal/sap/session"
	"github.com/habitat-network/habitat/internal/sap/syncer"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

type Config struct {
	DB          *gorm.DB
	OAuthClient *oauth.ClientApp

	// Directory resolves identities for commit signature verification: the
	// author's own key for habitat-managed authors, the host's published key
	// for external ones. When nil, commits are verified by hash only.
	Directory identity.Directory

	// Endpoint is sap's public base URL, registered with space hosts as the
	// destination for notifyWrite / notifySpaceDeleted. When empty, sap does
	// not register for notifications; the caller must route them some other
	// way.
	Endpoint string

	// Parallelism is the sync worker pool size (default 5).
	Parallelism int

	// CrawlInterval is how often each session is re-crawled to discover spaces
	// created since the last crawl (default 1h).
	CrawlInterval time.Duration

	Meter  metric.Meter
	Tracer trace.Tracer
}

// Sap composes the sync components behind a small façade. Components live in
// their own packages and interact only through interfaces; Sap wires them.
type Sap struct {
	db            *gorm.DB
	sessions      *session.Store
	crawler       *crawl.Crawler
	engine        *syncer.Engine
	registrar     *register.Registrar // nil when Config.Endpoint is empty
	outbox        *outbox.Store
	crawlInterval time.Duration
	tracer        trace.Tracer
}

func New(config Config) (*Sap, error) {
	for _, migrate := range []func(*gorm.DB) error{
		session.AutoMigrate,
		crawl.AutoMigrate,
		syncer.AutoMigrate,
		register.AutoMigrate,
		outbox.AutoMigrate,
	} {
		if err := migrate(config.DB); err != nil {
			return nil, fmt.Errorf("migrate database: %w", err)
		}
	}

	tracer := config.Tracer
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("sap")
	}

	sessions := session.NewStore(config.DB, config.OAuthClient)
	ob := outbox.NewStore(config.DB, utils.NewPollNotifier())

	syncMetrics, err := syncer.NewMetrics(config.Meter, config.Tracer)
	if err != nil {
		return nil, fmt.Errorf("create syncer metrics: %w", err)
	}
	engine := syncer.New(
		config.DB,
		sessions,
		outboxEmitter{store: ob},
		syncer.NewVerifier(config.Directory),
		config.Parallelism,
		syncMetrics,
	)

	var registrar *register.Registrar
	// crawl.Notify must stay a typed-nil-free interface value when
	// registration is disabled.
	var crawlNotify crawl.Notify
	if config.Endpoint != "" {
		registrar = register.New(config.DB, sessions, sessions, config.Endpoint)
		crawlNotify = registrar
	}

	crawler, err := crawl.New(
		config.DB,
		sessions,
		sessions,
		engine,
		crawlNotify,
		config.Meter,
		config.Tracer,
	)
	if err != nil {
		return nil, fmt.Errorf("create crawler: %w", err)
	}

	crawlInterval := config.CrawlInterval
	if crawlInterval <= 0 {
		crawlInterval = time.Hour
	}

	return &Sap{
		db:            config.DB,
		sessions:      sessions,
		crawler:       crawler,
		engine:        engine,
		registrar:     registrar,
		outbox:        ob,
		crawlInterval: crawlInterval,
		tracer:        tracer,
	}, nil
}

// Start runs the background loops (sync engine, crawl resumption and periodic
// re-crawls, notify registration upkeep) until ctx ends.
func (s *Sap) Start(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		s.engine.Run(ctx)
		return nil
	})
	eg.Go(func() error {
		return s.crawler.ResumeIncomplete(ctx)
	})
	eg.Go(func() error {
		s.recrawlLoop(ctx)
		return nil
	})
	if s.registrar != nil {
		eg.Go(func() error {
			s.registrar.Run(ctx)
			return nil
		})
	}
	return eg.Wait()
}

// recrawlLoop periodically re-crawls every session so spaces created since
// the last crawl are discovered and registered for notifications.
func (s *Sap) recrawlLoop(ctx context.Context) {
	ticker := time.NewTicker(s.crawlInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dids, err := s.sessions.List(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "recrawl: list sessions", "err", err)
				continue
			}
			for _, did := range dids {
				s.crawler.Run(ctx, did)
			}
		}
	}
}

// AddSession registers an authenticated session (after the caller completed
// the OAuth flow) and kicks off its backfill crawl in the background.
func (s *Sap) AddSession(ctx context.Context, did syntax.DID, sessionID string) error {
	if err := s.sessions.Add(ctx, did, sessionID); err != nil {
		return fmt.Errorf("add session: %w", err)
	}
	go s.crawler.Run(detachSpan(ctx), did)
	return nil
}

// Sessions lists the DIDs of the sessions sap syncs on behalf of.
func (s *Sap) Sessions(ctx context.Context) ([]syntax.DID, error) {
	return s.sessions.List(ctx)
}

// NotifyWrite reacts to a host's notifyWrite: the repo advanced to rev with
// commit hash (sha256 of its LtHash state; may be nil). The repo is synced
// incrementally and re-verified.
func (s *Sap) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
) error {
	return s.engine.NotifyWrite(ctx, space, repo, rev, hash)
}

// NotifySpaceDeleted reacts to a host's notifySpaceDeleted: all local tracking
// state for the space is dropped.
func (s *Sap) NotifySpaceDeleted(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.engine.WithTx(tx).DropSpace(ctx, space); err != nil {
			return fmt.Errorf("drop repos: %w", err)
		}
		if err := s.sessions.WithTx(tx).DropSpace(ctx, space); err != nil {
			return fmt.Errorf("drop space access: %w", err)
		}
		if s.registrar != nil {
			if err := s.registrar.WithTx(tx).DropSpace(ctx, space); err != nil {
				return fmt.Errorf("drop registration: %w", err)
			}
		}
		return nil
	})
}

// Outbox exposes the acknowledged delivery stream of synced records.
func (s *Sap) Outbox() outbox.Outbox {
	return s.outbox
}

// outboxEmitter adapts outbox.Store to syncer.Emitter.
type outboxEmitter struct {
	store *outbox.Store
}

func (e outboxEmitter) Emit(
	ctx context.Context,
	uri habitat_syntax.SpaceRecordURI,
	value []byte,
) error {
	return e.store.Emit(ctx, uri, value)
}

func (e outboxEmitter) InTx(tx *gorm.DB) syncer.Emitter {
	return outboxEmitter{store: e.store.WithTx(tx)}
}

// detachSpan returns a context that carries the trace span from ctx as a
// remote parent but is not bound to ctx's cancellation or deadline, for
// fire-and-forget goroutines that outlive the calling request.
func detachSpan(ctx context.Context) context.Context {
	return trace.ContextWithRemoteSpanContext(
		context.Background(),
		trace.SpanContextFromContext(ctx),
	)
}
