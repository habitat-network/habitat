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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/sync/errgroup"
	"gorm.io/gorm"
)

type Sap interface {
	http.Handler
	Start(ctx context.Context) error
	AddOrg(ctx context.Context, orgIdenitifier string) (redirectURL string, err error)
	ListOrgs(ctx context.Context) ([]syntax.DID, error)
}

type sapImpl struct {
	*orgManager
	db         *gorm.DB
	pathPrefix string
	sub        *subscriber
	resyncBuf  *resyncBuffer
	resyncer   *resyncer
	crawler    *crawler
	metrics    *metrics
}

type SapConfig struct {
	PublicDomain      string
	Secret            string
	DB                *gorm.DB
	ResyncParallelism int
	Directory         identity.Directory
	Meter             metric.Meter
	Tracer            trace.Tracer
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

	dir := config.Directory
	if dir == nil {
		dir = identity.DefaultDirectory()
	}

	m, err := newMetrics(config.Meter, config.Tracer)
	if err != nil {
		return nil, fmt.Errorf("failed to create metrics: %w", err)
	}

	o := newOrgManager(config.DB, config.PublicDomain, secret, dir)
	resyncBuf := newResyncBuffer(config.DB, resyncNotifCh)
	sub := newSubscriber(config.DB, o, resyncBuf, m)
	resyncer := newResyncer(config.DB, o, resyncBuf, resyncNotifCh, config.ResyncParallelism, m)
	crawler := newCrawler(config.DB, o, resyncBuf, sub, resyncNotifCh, m)

	_, pathPrefix, _ := strings.Cut(config.PublicDomain, "/")
	return &sapImpl{
		orgManager: o,
		db:         config.DB,
		pathPrefix: pathPrefix,
		sub:        sub,
		resyncBuf:  resyncBuf,
		resyncer:   resyncer,
		crawler:    crawler,
		metrics:    m,
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
