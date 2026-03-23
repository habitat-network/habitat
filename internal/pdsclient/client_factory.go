package pdsclient

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdscred"
	lru "github.com/hashicorp/golang-lru/v2"
)

type HttpClientFactory interface {
	NewClient(ctx context.Context, did syntax.DID) (HttpClient, error)
}

// clientFactoryImpl helps create clients to make authenticated requests to a DID's atproto PDS
type clientFactoryImpl struct {
	credStore   pdscred.PDSCredentialStore
	oauthClient PdsOAuthClient
	dir         identity.Directory

	dpopClientCache *lru.Cache[syntax.DID, *authedDpopHttpClient]
}

func NewHttpClientFactory(
	credStore pdscred.PDSCredentialStore,
	oauthClient PdsOAuthClient,
	dir identity.Directory,
) (HttpClientFactory, error) {
	cache, err := lru.New[syntax.DID, *authedDpopHttpClient](1024 /* arbitrary size for now */)
	if err != nil {
		return nil, err
	}
	return &clientFactoryImpl{
		credStore:       credStore,
		oauthClient:     oauthClient,
		dir:             dir,
		dpopClientCache: cache,
	}, nil
}

type HttpClient interface {
	Do(req *http.Request) (*http.Response, error)
}

func (f *clientFactoryImpl) NewClient(
	ctx context.Context,
	did syntax.DID,
) (HttpClient, error) {

	// Use context.Background() to avoid cached context cancelled errors: https://github.com/bluesky-social/indigo/pull/1345
	// Wasteful: we always create an unused throwaway client even if there is something in the cache already
	id, err := f.dir.LookupDID(context.Background(), did)
	if err != nil {
		return nil, fmt.Errorf("[pds client factory]: failed to lookup did: error is %w", err)
	}

	client, _, _ := f.dpopClientCache.PeekOrAdd(did, newAuthedDpopHttpClient(id, f.credStore, f.oauthClient, &MemoryNonceProvider{}))
	return client, nil
}
