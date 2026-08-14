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
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/sap/crawl"
	"github.com/habitat-network/habitat/pkg/sap/credential"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
	"github.com/habitat-network/habitat/pkg/sap/register"
	"github.com/habitat-network/habitat/pkg/sap/session"
	"github.com/habitat-network/habitat/pkg/sap/syncer"
)

type Config struct {
	DB *gorm.DB

	// OAuthClient resumes sessions AddSession registers, via its Store. sap
	// itself doesn't know how a session was established (browser OAuth
	// code flow, JWT-bearer grant, ...) — that's entirely the caller's
	// concern; the caller only needs to get a session into OAuthClient's
	// Store (however it does that) and pass its session ID to AddSession.
	OAuthClient *oauth.ClientApp

	// Directory resolves identities for commit signature verification (the
	// author's own key for habitat-managed authors, the host's published key
	// for external ones) and for finding a space's own host when minting a
	// space credential to read it. Required for sap to actually sync
	// anything; when nil, commits are verified by hash only and every
	// repo-host read fails for lack of a credential.
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
	credentials   *credential.Manager
	crawler       *crawl.Crawler
	engine        *syncer.Engine
	registrar     *register.Registrar // nil when Config.Endpoint is empty
	outbox        *outbox.Store
	tracer        trace.Tracer
	crawlInterval time.Duration
}

func New(config Config) (*Sap, error) {
	tracer := config.Tracer
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("sap")
	}

	sessions, err := session.NewStore(config.DB, config.OAuthClient)
	if err != nil {
		return nil, fmt.Errorf("create session store: %w", err)
	}
	// credentials mints and caches space credentials, one per space regardless
	// of which session was used to obtain it — a space credential authorizes
	// the space, not the member who fetched it. It asks sessions (which
	// implements credential.Delegator) for a delegation token on demand.
	credentials := credential.NewManager(config.Directory, httpx.NewClient(), sessions)
	ob, err := outbox.NewStore(config.DB, utils.NewPollNotifier())
	if err != nil {
		return nil, fmt.Errorf("create outbox store: %w", err)
	}

	syncMetrics, err := syncer.NewMetrics(config.Meter, config.Tracer)
	if err != nil {
		return nil, fmt.Errorf("create syncer metrics: %w", err)
	}
	engine, err := syncer.New(
		config.DB,
		credentials,
		outboxEmitter{store: ob},
		syncer.NewVerifier(config.Directory),
		config.Parallelism,
		syncMetrics,
	)
	if err != nil {
		return nil, fmt.Errorf("create syncer: %w", err)
	}

	var registrar *register.Registrar
	// crawl.Notify must stay a typed-nil-free interface value when
	// registration is disabled.
	var crawlNotify crawl.Notify
	if config.Endpoint != "" {
		registrar, err = register.New(config.DB, credentials, sessions, config.Endpoint)
		if err != nil {
			return nil, fmt.Errorf("create registrar: %w", err)
		}
		crawlNotify = registrar
	}

	crawler, err := crawl.New(
		config.DB,
		config.OAuthClient,
		sessions,
		credentials,
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
		credentials:   credentials,
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
// the last crawl are discovered and registered for notifications. A finished
// crawl restarts from the top, so this also re-lists every already-tracked
// repo and, via the engine's Check, is what converges a repo whose
// notifyWrite was dropped rather than leaving it at a stale rev forever.
func (s *Sap) recrawlLoop(ctx context.Context) {
	ticker := time.NewTicker(s.crawlInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			sessions, err := s.sessions.List(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "recrawl: list sessions", "err", err)
				continue
			}
			for _, sess := range sessions {
				s.crawler.Run(ctx, sess.DID, sess.SessionID)
			}
		}
	}
}

// AddSession registers a session, resumable via sessionID through
// Config.OAuthClient, and kicks off its backfill crawl.
func (s *Sap) AddSession(ctx context.Context, did syntax.DID, sessionID string) error {
	if err := s.sessions.Add(ctx, did, sessionID); err != nil {
		return fmt.Errorf("add session: %w", err)
	}
	go s.crawler.Run(detachSpan(ctx), did, sessionID)
	return nil
}

// Sessions lists the DIDs of the sessions sap syncs on behalf of.
func (s *Sap) Sessions(ctx context.Context) ([]syntax.DID, error) {
	sessions, err := s.sessions.List(ctx)
	if err != nil {
		return nil, err
	}
	dids := make([]syntax.DID, len(sessions))
	for i, sess := range sessions {
		dids[i] = sess.DID
	}
	return dids, nil
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

// TrackSpace tracks a space sap wouldn't discover through a session's crawl —
// e.g. one the caller learned about out of band (an invite, an external
// notification). It records that (did, sessionID) can access the space,
// registers the space for notifications, and syncs its repos, exactly as if
// the session's listSpaces had returned it.
func (s *Sap) TrackSpace(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	did syntax.DID,
	sessionID string,
) error {
	return s.crawler.TrackSpace(ctx, space, did, sessionID)
}

// NotifySpaceDeleted reacts to a host's notifySpaceDeleted: all local tracking
// state for the space is dropped.
func (s *Sap) NotifySpaceDeleted(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) error {
	s.credentials.DropSpace(space)
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
