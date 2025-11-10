package oauthserver

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/stretchr/testify/require"
)

type dummyOAuthClient struct {
	metadata *auth.ClientMetadata
	server   *httptest.Server
	t        *testing.T
}

func NewDummyOAuthClient(t *testing.T, metadata *auth.ClientMetadata) *dummyOAuthClient {
	client := &dummyOAuthClient{
		metadata: metadata,
		t:        t,
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			{
				redirect := r.URL.Query().Get("redirect_uri")
				q := url.Values{
					"code":  []string{"dummyCode"},
					"state": []string{r.URL.Query().Get("state")},
				}
				w.Header().Add("Location", redirect+"?"+q.Encode())
				w.WriteHeader(http.StatusSeeOther)
			}
		}
	}))
	client.server = server
	return client
}

var _ auth.OAuthClient = (*dummyOAuthClient)(nil)

// Authorize implements OAuthClient.
func (d *dummyOAuthClient) Authorize(
	_ *auth.DpopHttpClient,
	i *identity.Identity,
) (string, *auth.AuthorizeState, error) {
	q := url.Values{
		"redirect_uri": []string{d.metadata.RedirectUris[0]},
	}
	return d.server.URL + "/authorize?" + q.Encode(), &auth.AuthorizeState{
		Verifier:      "dummyVerifier",
		State:         "dummyState",
		TokenEndpoint: d.server.URL + "/token",
	}, nil
}

// ClientMetadata implements OAuthClient.
func (d *dummyOAuthClient) ClientMetadata() *auth.ClientMetadata {
	return d.metadata
}

// ExchangeCode implements OAuthClient.
func (d *dummyOAuthClient) ExchangeCode(
	dpopClient *auth.DpopHttpClient,
	code string,
	issuer string,
	state *auth.AuthorizeState,
) (*auth.TokenResponse, error) {
	require.Equal(d.t, "dummyCode", code)
	require.Equal(d.t, "dummyState", state.State)
	require.Equal(d.t, "dummyVerifier", state.Verifier)
	return &auth.TokenResponse{
		AccessToken:  "dummy_access_token",
		RefreshToken: "dummy_refresh_token",
		TokenType:    "DPoP",
		ExpiresIn:    3600,
		Scope:        "atproto transition:generic",
	}, nil
}

// RefreshToken implements OAuthClient.
func (d *dummyOAuthClient) RefreshToken(
	dpopClient *auth.DpopHttpClient,
	identity *identity.Identity,
	issuer string,
	refreshToken string,
) (*auth.TokenResponse, error) {
	require.Equal(d.t, "dummy_refresh_token", refreshToken)
	return &auth.TokenResponse{
		AccessToken:  "dummy_refreshed_access_token",
		RefreshToken: "dummy_refresh_token",
		TokenType:    "DPoP",
		ExpiresIn:    3600,
		Scope:        "atproto transition:generic",
	}, nil
}

func (d *dummyOAuthClient) Close() {
	d.server.Close()
}
