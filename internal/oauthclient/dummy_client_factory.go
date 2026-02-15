package oauthclient

import (
	"context"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type DummyClientFactory struct {
	pdsUrl string
}

func NewDummyClientFactory(pdsUrl string) *DummyClientFactory {
	return &DummyClientFactory{pdsUrl}
}

var _ PDSClientFactory = &DummyClientFactory{}

// NewClient implements [PDSClientFactory].
func (d *DummyClientFactory) NewClient(ctx context.Context, did syntax.DID) (PDSClient, error) {
	return &dummyClient{d.pdsUrl}, nil
}

type dummyClient struct{ pdsUrl string }

// Do implements [PDSClient].
func (d *dummyClient) Do(req *http.Request) (*http.Response, error) {
	pdsUrl, err := url.Parse(d.pdsUrl)
	if err != nil {
		return nil, err
	}
	req.URL = pdsUrl.ResolveReference(req.URL)
	return http.DefaultClient.Do(req)
}
