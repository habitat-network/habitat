package sap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/habitat-network/habitat/internal/oauth_client"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type Sap interface {
	Start(ctx context.Context) error
	StartOrgSync(did syntax.DID)
	Outbox
}

type sapImpl struct {
	oauthClient *oauth_client.App
	Outbox
	db        *gorm.DB
	sub       *subscriber
	resyncBuf *resyncBuffer
	resyncer  *resyncer
	crawler   *crawler
}

type SapConfig struct {
	PublicDomain      string
	Secret            string
	DB                *gorm.DB
	ResyncParallelism int
	Directory         identity.Directory
	OAuthClient       *oauth_client.App
}

func NewSap(config SapConfig) (Sap, error) {
	if err := autoMigrate(config.DB); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}

	resyncNotif := utils.NewPollNotifier()
	outboxNotif := utils.NewPollNotifier()

	resyncBuf := newResyncBuffer(config.DB, resyncNotif, outboxNotif)
	sub := newSubscriber(config.DB, config.OAuthClient, resyncBuf)
	resyncer := newResyncer(
		config.DB,
		config.OAuthClient,
		resyncBuf,
		resyncNotif,
		outboxNotif,
		config.ResyncParallelism,
	)
	crawler := newCrawler(config.DB, config.OAuthClient, resyncBuf, sub, resyncNotif)
	outbox := newOutbox(config.DB, outboxNotif)

	return &sapImpl{
		oauthClient: config.OAuthClient,
		Outbox:      outbox,
		db:          config.DB,
		sub:         sub,
		resyncBuf:   resyncBuf,
		resyncer:    resyncer,
		crawler:     crawler,
	}, nil
}

func (s *sapImpl) Start(ctx context.Context) error {
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

func (s *sapImpl) StartOrgSync(did syntax.DID) {
	var org managedOrg
	if err := s.db.Where("did = ?", did).First(&org).Error; err != nil {
		slog.Error("org not found", "did", did, "err", err)
		return
	}
	go s.crawler.crawlOrg(context.Background(), &org)
	go s.sub.addSubscription(context.Background(), &org)
}
