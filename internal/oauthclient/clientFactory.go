package oauthclient

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdscred"
)

// PDSClientFactory helps create clients to make authenticated requests to a DID's atproto PDS
type PDSClientFactory struct {
	credStore   pdscred.PDSCredentialStore
	oauthClient OAuthClient
	dir         identity.Directory
}

func NewPDSClientFactory(
	credStore pdscred.PDSCredentialStore,
	oauthClient OAuthClient,
	dir identity.Directory,
) *PDSClientFactory {
	return &PDSClientFactory{
		credStore:   credStore,
		oauthClient: oauthClient,
		dir:         dir,
	}
}

func (f *PDSClientFactory) NewClient(
	ctx context.Context,
	did syntax.DID,
) (*AuthedDpopHttpClient, error) {
	id, err := f.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup did: %w", err)
	}
	return NewAuthedDpopHttpClient(id, f.credStore, f.oauthClient, &MemoryNonceProvider{}), nil
}
