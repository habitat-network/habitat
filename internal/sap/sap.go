package sap

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

type Sap interface {
	SyncOrg(ctx context.Context, org syntax.DID, token oauth2.TokenSource)
}

type sapImpl struct {
	db *gorm.DB
}

func NewSap(db *gorm.DB) (Sap, error) {
	return &sapImpl{db: db}, nil
}

// SyncOrg implements [Sap].
func (s *sapImpl) SyncOrg(ctx context.Context, org syntax.DID, token oauth2.TokenSource) {
	panic("unimplemented")
}
