package oauthclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdscred"
)

type PDSClientFactory interface {
	NewClient(ctx context.Context, did syntax.DID) (PDSClient, error)
}

// pdsClientFactoryImpl helps create clients to make authenticated requests to a DID's atproto PDS
type pdsClientFactoryImpl struct {
	credStore   pdscred.PDSCredentialStore
	oauthClient OAuthClient
	dir         identity.Directory
}

func NewPDSClientFactory(
	credStore pdscred.PDSCredentialStore,
	oauthClient OAuthClient,
	dir identity.Directory,
) PDSClientFactory {
	return &pdsClientFactoryImpl{
		credStore:   credStore,
		oauthClient: oauthClient,
		dir:         dir,
	}
}

type PDSClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func (f *pdsClientFactoryImpl) NewClient(
	ctx context.Context,
	did syntax.DID,
) (PDSClient, error) {
	id, err := f.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup did: %w", err)
	}
	return newAuthedDpopHttpClient(id, f.credStore, f.oauthClient, &MemoryNonceProvider{}), nil
}
