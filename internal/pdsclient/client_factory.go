package pdsclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdscred"
)

type HttpClientFactory interface {
	NewClient(ctx context.Context, did syntax.DID) (HttpClient, error)
}

// clientFactoryImpl helps create clients to make authenticated requests to a DID's atproto PDS
type clientFactoryImpl struct {
	credStore   pdscred.PDSCredentialStore
	oauthClient PdsOAuthClient
	dir         identity.Directory
}

func NewHttpClientFactory(
	credStore pdscred.PDSCredentialStore,
	oauthClient PdsOAuthClient,
	dir identity.Directory,
) HttpClientFactory {
	return &clientFactoryImpl{
		credStore:   credStore,
		oauthClient: oauthClient,
		dir:         dir,
	}
}

type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func (f *clientFactoryImpl) NewClient(
	ctx context.Context,
	did syntax.DID,
) (HttpClient, error) {
	id, err := f.dir.LookupDID(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("[pds client factory]: failed to lookup did: error is %w", err)
	}
	return newAuthedDpopHttpClient(id, f.credStore, f.oauthClient, &MemoryNonceProvider{}), nil
}
