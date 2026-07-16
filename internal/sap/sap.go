// Package sap implements the Sync Agent Process for Habitat. It crawls
// managed organizations' AT Protocol repos, discovers spaces and their member
// repositories, and keeps a local copy of each repo's records in sync via
// live SSE subscriptions and periodic resyncs. The synced records are exposed
// through a durable [Outbox] that consumers can poll for new events.
//
// # Lifecycle
//
// A typical integration creates a [Sap] with [NewSap], calls [Sap.AddManagedOrg]
// for each organization the instance manages, and then calls [Sap.Start] to
// begin background crawling, subscribing, and resyncing. The outbox is
// accessible at any time through [Sap.Outbox].
//
// # Architecture
//
// The package is built around four cooperating subsystems:
//
//   - **Crawler** – performs a one-shot enumeration of every space and repo in
//     a newly added org. Repos discovered during crawling are enqueued for
//     resync.
//   - **Subscriber** – opens a persistent SSE connection per org to receive
//     real-time space events. Incoming events are either applied immediately
//     (for repos that are already active) or buffered for later replay.
//   - **Resyncer** – a worker pool that backfills repos from scratch or
//     catches up repos that fell behind. It pulls work from a shared job
//     queue that is fed by both the crawler and the subscriber.
//   - **Outbox** – the consumer-facing API. It exposes a durable, ordered
//     stream of record-level events that have been validated and written by
//     the resyncer or subscriber.
//
// A [resyncBuffer] sits between the subscriber and the outbox, deciding per
// event whether to apply it directly or hold it in a pending buffer while a
// resync is in flight.
package sap

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

// Sap is the top-level Sync Agent Process. It manages the full lifecycle of
// crawling, subscribing, and resyncing orgs, and exposes the resulting
// record stream through [Sap.Outbox].
type Sap struct {
	// Outbox delivers synced record events to consumers. Messages are
	// redelivered until acknowledged via [Outbox.Ack].
	Outbox      Outbox
	oauthClient *oauthclient.App
	db          *gorm.DB
	sub         *subscriber
	resyncBuf   *resyncBuffer
	resyncer    *resyncer
	crawler     *crawler
	orgManager  *orgManager
	metrics     *metrics
}

// SapConfig holds the dependencies needed to construct a [Sap].
type SapConfig struct {
	// DB is the GORM database handle used for all persistent state. The
	// schema is auto-migrated on [NewSap].
	DB *gorm.DB
	// ResyncParallelism controls how many repo resync workers run
	// concurrently. Defaults to 5 when set to zero or negative.
	ResyncParallelism int
	// Directory resolves AT Protocol DIDs to their PDS endpoints.
	Directory identity.Directory
	// OAuthClient provides authenticated HTTP clients for calling org PDSes.
	OAuthClient       *oauthclient.App
	// Meter is the OpenTelemetry meter for recording metrics. Pass nil to
	// use a no-op meter.
	Meter metric.Meter
	// Tracer is the OpenTelemetry tracer for creating spans. Pass nil to
	// use a no-op tracer.
	Tracer trace.Tracer
}

// NewSap creates a new Sap, auto-migrating the database schema and
// initializing all internal subsystems (subscriber, crawler, resyncer, outbox).
func NewSap(config SapConfig) (*Sap, error) {
	if err := autoMigrate(config.DB); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	m, err := newMetrics(config.Meter, config.Tracer)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()

	resyncBuf := newResyncBuffer(config.DB, resyncNotif, outboxNotif)
	sub := newSubscriber(config.DB, config.OAuthClient, resyncBuf, m)
	resyncer := newResyncer(
		config.DB,
		config.OAuthClient,
		resyncBuf,
		resyncNotif,
		outboxNotif,
		config.ResyncParallelism,
		m,
	)
	crawler := newCrawler(config.DB, config.OAuthClient, resyncBuf, sub, resyncNotif, m)
	outbox := newOutbox(config.DB, outboxNotif)
	orgManager := newOrgManager(config.DB)

	return &Sap{
		orgManager:  orgManager,
		oauthClient: config.OAuthClient,
		Outbox:      outbox,
		db:          config.DB,
		sub:         sub,
		resyncBuf:   resyncBuf,
		resyncer:    resyncer,
		crawler:     crawler,
		metrics:     m,
	}, nil
}

// Start begins background processing for all managed orgs. It loads existing
// subscriptions, resumes any incomplete crawls, and runs the resyncer until
// ctx is cancelled. Start blocks until all background goroutines finish and
// returns any error from the subscriber's shutdown (cursors are persisted
// per-event, so errors here are typically nil).
func (s *Sap) Start(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		return s.sub.loadSubscriptions(ctx)
	})

	eg.Go(func() error {
		return s.crawler.resumeIncompleteCrawls(ctx)
	})

	eg.Go(func() error {
		s.resyncer.run(ctx)
		return nil
	})

	err := eg.Wait()
	return errors.Join(err, s.sub.closeSubscriptions())
}

// AddManagedOrg registers a new organization for sync. did is the org's AT
// Protocol DID and sessionID is the OAuth session identifier that will be
// used to authenticate requests to the org's PDS. After registration the
// org is immediately crawled (discovering its spaces and repos) and a live
// subscription is opened for real-time events.
func (s *Sap) AddManagedOrg(ctx context.Context, did syntax.DID, sessionID string) error {
	org, err := s.orgManager.AddManagedOrg(ctx, did, sessionID)
	if err != nil {
		return err
	}
	go s.crawler.crawlOrg(detachSpan(ctx), org)
	go s.sub.addSubscription(detachSpan(ctx), org)
	return nil
}

// ListManagedOrgs returns the DIDs of all organizations currently registered
// with this Sap instance.
func (s *Sap) ListManagedOrgs(ctx context.Context) ([]syntax.DID, error) {
	return s.orgManager.ListManagedOrgs(ctx)
}

// GetClient returns an HTTP client that authenticates as the given managed org
// DID using the OAuth session sap tracks for it. Requests made with the
// returned client are resolved against the org's Habitat (pear) host and carry
// the org's access token.
func (s *Sap) GetClient(ctx context.Context, did syntax.DID) (*http.Client, error) {
	org, err := s.orgManager.GetManagedOrg(ctx, did)
	if err != nil {
		return nil, err
	}
	return s.oauthClient.GetClient(ctx, did, org.SessionID)
}
