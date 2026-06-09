package pdsclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"
)

type DummyOAuthClient struct {
	metadata *ClientMetadata
	server   *httptest.Server
	t        *testing.T
}

func NewDummyOAuthClient(t *testing.T, metadata *ClientMetadata) *DummyOAuthClient {
	client := &DummyOAuthClient{
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

var _ PdsOAuthClient = (*DummyOAuthClient)(nil)

func (d *DummyOAuthClient) Authorize(
	_ context.Context,
	_ *DpopHttpClient,
	i *identity.Identity,
) (string, *AuthorizeState, error) {
	q := url.Values{
		"redirect_uri": []string{d.metadata.RedirectUris[0]},
	}
	return d.server.URL + "/authorize?" + q.Encode(), &AuthorizeState{
		Verifier:      "dummyVerifier",
		State:         "dummyState",
		TokenEndpoint: d.server.URL + "/token",
	}, nil
}

func (d *DummyOAuthClient) ClientMetadata() *ClientMetadata {
	return d.metadata
}

func (d *DummyOAuthClient) ExchangeCode(
	dpopClient *DpopHttpClient,
	code string,
	issuer string,
	state *AuthorizeState,
) (*TokenResponse, error) {
	require.Equal(d.t, "dummyCode", code)
	require.Equal(d.t, "dummyState", state.State)
	require.Equal(d.t, "dummyVerifier", state.Verifier)
	sig, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.HS256, Key: []byte("dummySecret")},
		nil,
	)
	require.NoError(d.t, err)
	token, err := jwt.Signed(sig).Claims(jwt.Claims{
		Subject: "did:web:example.did.com",
		Expiry:  jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}).CompactSerialize()
	require.NoError(d.t, err)
	return &TokenResponse{
		AccessToken:  token,
		RefreshToken: "dummy_refresh_token",
		TokenType:    "DPoP",
		ExpiresIn:    3600,
		Scope:        "atproto transition:generic",
	}, nil
}

func (d *DummyOAuthClient) RefreshToken(
	dpopClient *DpopHttpClient,
	identity *identity.Identity,
	issuer string,
	refreshToken string,
) (*TokenResponse, error) {
	require.Equal(d.t, "dummy_refresh_token", refreshToken)
	return &TokenResponse{
		AccessToken:  "dummy_refreshed_access_token",
		RefreshToken: "dummy_refresh_token",
		TokenType:    "DPoP",
		ExpiresIn:    3600,
		Scope:        "atproto transition:generic",
	}, nil
}

func (d *DummyOAuthClient) Close() {
	d.server.Close()
}
