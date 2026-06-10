package sap

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

type Sap interface {
	http.Handler
	AddOrg(ctx context.Context, orgIdenitifier string) (redirectURL string, err error)
	ListOrgs(ctx context.Context) ([]syntax.DID, error)
}

type sapImpl struct {
	*orgManager
	db         *gorm.DB
	pathPrefix string
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
	_, pathPrefix, _ := strings.Cut(config.PublicDomain, "/")
	return &sapImpl{
		orgManager: o,
		db:         config.DB,
		pathPrefix: pathPrefix,
	}, nil
}

func (s *sapImpl) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	srv := &server{sap: s}
	srv.ServeHTTP(w, r)
}
