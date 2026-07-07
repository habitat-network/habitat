package sap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/utils"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type Sap struct {
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

type SapConfig struct {
	DB                *gorm.DB
	ResyncParallelism int
	Directory         identity.Directory
	OAuthClient       *oauthclient.App
	Meter             metric.Meter
	Tracer            trace.Tracer
}

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

func (s *Sap) AddManagedOrg(ctx context.Context, did syntax.DID, sessionID string) error {
	org, err := s.orgManager.AddManagedOrg(ctx, did, sessionID)
	if err != nil {
		return err
	}
	go s.crawler.crawlOrg(detachSpan(ctx), org)
	go s.sub.addSubscription(detachSpan(ctx), org)
	return nil
}

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

func (s *Sap) HandleNotifyApp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatOrgNotifyAppInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.LogAndHTTPError(ctx, w, err, "decode input", http.StatusBadRequest)
		return
	}

	orgDID, err := syntax.ParseDID(input.Org)
	if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "parse org did", http.StatusBadRequest)
		return
	}

	_, err = s.orgManager.GetManagedOrg(ctx, orgDID)
	if errors.Is(err, ErrOrgNotFound) {
		// if we receive a notification about an org that we don't manage, add it
		// using a jwt bearer grant. the assumption is that pear trusts this app so we
		// should be able to receive an org credential
		slog.InfoContext(ctx, "org not managed, adding", "org", orgDID)
		session, err := s.oauthClient.AddSessionWithBearerJwt(ctx, orgDID)
		if err != nil {
			utils.LogAndHTTPError(ctx, w, err, "add session", http.StatusInternalServerError)
			return
		}
		if err := s.AddManagedOrg(ctx, session.AccountDID, session.SessionID); err != nil {
			utils.LogAndHTTPError(ctx, w, err, "add org", http.StatusInternalServerError)
			return
		}
	} else if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "get org", http.StatusInternalServerError)
		return
	}

	// TODO: kick off / resume syncing the org's repos now that it is managed.
}
