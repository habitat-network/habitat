package sap

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type Sap interface {
	http.Handler
	Start(ctx context.Context) error
	AddOrg(ctx context.Context, orgIdenitifier string) (redirectURL string, err error)
	ListOrgs(ctx context.Context) ([]syntax.DID, error)
	// Outbox exposes durable, ordered delivery of repo events to library consumers.
	Outbox
}

type sapImpl struct {
	*orgManager
	Outbox
	db         *gorm.DB
	pathPrefix string
	sub        *subscriber
	resyncBuf  *resyncBuffer
	resyncer   *resyncer
	crawler    *crawler
}

type SapConfig struct {
	PublicDomain      string
	Secret            string
	DB                *gorm.DB
	ResyncParallelism int
	Directory         identity.Directory
}

func NewSap(config SapConfig) (Sap, error) {
	if err := autoMigrate(config.DB); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	secret, err := atcrypto.ParsePrivateMultibase(config.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret: %w", err)
	}

	resyncNotifCh := make(chan struct{}, 1)
	outboxNotifyCh := make(chan struct{}, 1)

	dir := config.Directory
	if dir == nil {
		dir = identity.DefaultDirectory()
	}

	o := newOrgManager(config.DB, config.PublicDomain, secret, dir)
	resyncBuf := newResyncBuffer(config.DB, resyncNotifCh, outboxNotifyCh)
	sub := newSubscriber(config.DB, o, resyncBuf)
	resyncer := newResyncer(
		config.DB,
		o,
		resyncBuf,
		resyncNotifCh,
		outboxNotifyCh,
		config.ResyncParallelism,
	)
	crawler := newCrawler(config.DB, o, resyncBuf, sub, resyncNotifCh)
	outbox := newOutbox(config.DB, outboxNotifyCh)

	_, pathPrefix, _ := strings.Cut(config.PublicDomain, "/")
	return &sapImpl{
		orgManager: o,
		Outbox:     outbox,
		db:         config.DB,
		pathPrefix: pathPrefix,
		sub:        sub,
		resyncBuf:  resyncBuf,
		resyncer:   resyncer,
		crawler:    crawler,
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

func (s *sapImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv := &server{sap: s}
	srv.ServeHTTP(w, r)
}

func (s *sapImpl) startOrgSync(org *managedOrg) {
	go s.crawler.crawlOrg(context.Background(), org)
	go s.sub.addSubscription(context.Background(), org)
}
