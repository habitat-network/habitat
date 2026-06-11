package sap

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/r3labs/sse/v2"
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
	sseCh      chan *sse.Event
}

type SapConfig struct {
	PublicDomain string
	Secret       string
	DB           *gorm.DB
}

func NewSap(config SapConfig) (Sap, error) {
	secret, err := atcrypto.ParsePrivateMultibase(config.Secret)
	if err != nil {
		return nil, fmt.Errorf("failed to parse secret: %w", err)
	}
	o, err := newOrgManager(config.DB, config.PublicDomain, secret)
	if err != nil {
		return nil, fmt.Errorf("failed to create org manager: %w", err)
	}

	sseCh := make(chan *sse.Event)

	_, pathPrefix, _ := strings.Cut(config.PublicDomain, "/")
	return &sapImpl{
		orgManager: o,
		db:         config.DB,
		pathPrefix: pathPrefix,
		sub:        newSubscriber(config.DB, o, sseCh),
		sseCh:      sseCh,
	}, nil
}

func (s *sapImpl) Start(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		err := s.sub.loadSubscriptions(ctx)
		if err != nil {
			return err
		}
		// TODO: retry failed orgs
		return nil
	})

	eg.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return nil
			case event := <-s.sseCh:
				slog.InfoContext(ctx, "received event", "event", event)
			}
		}
	})

	err := eg.Wait()
	return errors.Join(err, s.sub.closeSubscriptions())
}

func (s *sapImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv := &server{sap: s}
	srv.ServeHTTP(w, r)
}
