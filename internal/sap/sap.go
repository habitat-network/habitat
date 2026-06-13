package sap

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	repos      *repoManager
	resyncBuf  *resyncBuffer
	resyncer   *resyncer
	crawler    *crawler
}

type SapConfig struct {
	PublicDomain      string
	Secret            string
	DB                *gorm.DB
	ResyncParallelism int
}

func NewSap(config SapConfig) (Sap, error) {
	if err := autoMigrate(config.DB); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	secret, err := atcrypto.ParsePrivateMultibase(config.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret: %w", err)
	}
	o := newOrgManager(config.DB, config.PublicDomain, secret)
	repos := newRepoManager(config.DB)
	resyncBuf := newResyncBuffer(config.DB, repos)
	sub := newSubscriber(config.DB, o, resyncBuf)
	resyncer := newResyncer(config.DB, o, repos, resyncBuf, config.ResyncParallelism)
	crawler := newCrawler(config.DB, o, repos, resyncBuf, sub)

	_, pathPrefix, _ := strings.Cut(config.PublicDomain, "/")
	return &sapImpl{
		orgManager: o,
		db:         config.DB,
		pathPrefix: pathPrefix,
		sub:        sub,
		repos:      repos,
		resyncBuf:  resyncBuf,
		resyncer:   resyncer,
		crawler:    crawler,
	}, nil
}

func (s *sapImpl) Start(ctx context.Context) error {
	if err := s.repos.ResetPartiallyResynced(ctx); err != nil {
		return fmt.Errorf("reset partially resynced repos: %w", err)
	}
	go func() {
		if err := s.sub.loadSubscriptions(ctx); err != nil {
			slog.ErrorContext(ctx, "load subscriptions", "err", err)
		}
	}()
	go s.resyncer.run(ctx)
	go s.crawler.resumeIncompleteCrawls(ctx)
	<-ctx.Done()
	return s.sub.closeSubscriptions()
}

func (s *sapImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv := &server{sap: s}
	srv.ServeHTTP(w, r)
}

func (s *sapImpl) startOrgSync(org *managedOrg) {
	go s.crawler.crawlOrg(context.Background(), org)
	go s.sub.addSubscription(context.Background(), org)
}
