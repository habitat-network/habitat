package pdsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

// DummyDID is the account DID that DummyOAuthClient reports for a completed
// login. Tests that seed members should use this as the member's login ID.
const DummyDID = syntax.DID("did:web:example.did.com")

// DummyOAuthClient is a test double for PdsOAuthClient. It stands in for a real
// atproto OAuth flow: Authorize redirects to an in-process "PDS" that
// immediately bounces back to the callback with a canned code, and ExchangeCode
// reports a fixed account DID. Do proxies requests to PDSURL (if set), standing
// in for an authenticated PDS session.
type DummyOAuthClient struct {
	metadata *oauth.ClientMetadata
	server   *httptest.Server
	// PDSURL, when set, is the base URL that Do proxies requests to.
	PDSURL string
	t      *testing.T
}

var _ PdsOAuthClient = (*DummyOAuthClient)(nil)

func NewDummyOAuthClient(t *testing.T, metadata *oauth.ClientMetadata) *DummyOAuthClient {
	client := &DummyOAuthClient{metadata: metadata, t: t}
	client.server = httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/authorize" {
				redirect := r.URL.Query().Get("redirect_uri")
				q := url.Values{
					"code":  []string{"dummyCode"},
					"iss":   []string{r.URL.Query().Get("iss")},
					"state": []string{r.URL.Query().Get("state")},
				}
				w.Header().Add("Location", redirect+"?"+q.Encode())
				w.WriteHeader(http.StatusSeeOther)
				return
			}
			w.WriteHeader(http.StatusNotFound)
		}),
	)
	return client
}

// ClientMetadata implements [PdsOAuthClient].
func (d *DummyOAuthClient) ClientMetadata() *oauth.ClientMetadata {
	return d.metadata
}

// Authorize implements [PdsOAuthClient]. It returns a redirect to the in-process
// authorize endpoint, which bounces back to the client's callback.
func (d *DummyOAuthClient) Authorize(_ context.Context, _ string) (string, error) {
	q := url.Values{}
	if len(d.metadata.RedirectURIs) > 0 {
		q.Set("redirect_uri", d.metadata.RedirectURIs[0])
	}
	return d.server.URL + "/authorize?" + q.Encode(), nil
}

// ExchangeCode implements [PdsOAuthClient].
func (d *DummyOAuthClient) ExchangeCode(
	_ context.Context,
	code, _, _ string,
) (syntax.DID, error) {
	if d.t != nil {
		require.Equal(d.t, "dummyCode", code)
	}
	return DummyDID, nil
}

// Do implements [PdsOAuthClient]. It proxies the request to PDSURL, resolving
// relative request URLs against it.
func (d *DummyOAuthClient) Do(
	_ context.Context,
	_ syntax.DID,
	req *http.Request,
) (*http.Response, error) {
	base, err := url.Parse(d.PDSURL)
	if err != nil {
		return nil, err
	}
	req.URL = base.ResolveReference(req.URL)
	return http.DefaultClient.Do(req)
}

func (d *DummyOAuthClient) Close() {
	d.server.Close()
}
