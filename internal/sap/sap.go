package sap

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	registrar   *registrar
	metrics     *metrics
}

type SapConfig struct {
	DB                *gorm.DB
	ResyncParallelism int
	Directory         identity.Directory
	OAuthClient       *oauthclient.App
	Meter             metric.Meter
	Tracer            trace.Tracer

	// NotifyAudience is sap's public base URL: the service-auth audience it
	// expects on inbound notifyWrite / notifySpaceDeleted, and the endpoint it
	// registers with space hosts. When empty (with no Directory), the notify
	// entry points are disabled.
	NotifyAudience string
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

	var registrar *registrar
	if config.Directory != nil && config.NotifyAudience != "" {
		registrar = newRegistrar(config.DB, config.OAuthClient, config.NotifyAudience)
	}

	return &Sap{
		orgManager:  orgManager,
		oauthClient: config.OAuthClient,
		Outbox:      outbox,
		db:          config.DB,
		sub:         sub,
		resyncBuf:   resyncBuf,
		resyncer:    resyncer,
		crawler:     crawler,
		registrar:   registrar,
		metrics:     m,
	}, nil
}

func (s *Sap) NotifyWrite(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	hash []byte,
) error {
	err := s.resyncer.enqueueRepo(ctx, space, repo, rev)
	if err != nil {
		return fmt.Errorf("enqueue repo: %w", err)
	}
	return nil
}

func (s *Sap) NotifySpaceDeleted(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("space = ?", space).Delete(&managedRepo{}).Error; err != nil {
			return fmt.Errorf("delete managed repos: %w", err)
		}
		if err := tx.Where("space = ?", space).Delete(&managedSpace{}).Error; err != nil {
			return fmt.Errorf("delete managed spaces: %w", err)
		}
		if err := tx.Where("space = ?", space).Delete(&bufferedEvent{}).Error; err != nil {
			return fmt.Errorf("delete buffered events: %w", err)
		}
		return nil
	})
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

	if s.registrar != nil {
		eg.Go(func() error {
			s.registrar.run(ctx)
			return nil
		})
	}

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
