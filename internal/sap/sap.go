package sap

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type Sap struct {
	Outbox      Outbox
	oauthClient *oauth.ClientApp
	db          *gorm.DB
	sub         *subscriber
	resyncBuf   *resyncBuffer
	resyncer    *resyncer
	crawler     *crawler
	orgManager  *orgManager
	metrics     *metrics
}

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
	OAuthClient *oauth.ClientApp
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

	sesssionGetter := newSessionGetter(config.OAuthClient)

	resyncBuf := newResyncBuffer(config.DB, resyncNotif, outboxNotif)
	sub := newSubscriber(config.DB, sesssionGetter, resyncBuf, m)
	resyncer := newResyncer(
		config.DB,
		sesssionGetter,
		resyncBuf,
		resyncNotif,
		outboxNotif,
		config.ResyncParallelism,
		m,
	)
	crawler := newCrawler(config.DB, sesssionGetter, resyncBuf, sub, resyncNotif, m)
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
// Protocol DID and sessionID is the OAuth session identifier from  oauthClient
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

// GetSession returns the OAuth session sap tracks for the given managed org
// DID, for making authenticated requests against the org's Habitat host.
func (s *Sap) GetSession(ctx context.Context, did syntax.DID) (*oauth.ClientSession, error) {
	org, err := s.orgManager.GetManagedOrg(ctx, did)
	if err != nil {
		return nil, err
	}
	return s.oauthClient.ResumeSession(ctx, did, org.SessionID)
}

// ListManagedOrgs returns the DIDs of all organizations currently registered
// with this Sap instance.
func (s *Sap) ListManagedOrgs(ctx context.Context) ([]syntax.DID, error) {
	return s.orgManager.ListManagedOrgs(ctx)
}
