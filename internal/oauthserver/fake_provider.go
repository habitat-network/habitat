package oauthserver

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/stretchr/testify/require"
)

type fakePDSProvider struct {
	t                 *testing.T
	server            *httptest.Server
	authorizeState    []byte
	redirectURI       string // callback URL for the OAuth server
	exchangeFn        func(ctx context.Context, did syntax.DID, code, issuer, oauthState string, state []byte) error
}

func newFakePDSProvider(t ...*testing.T) *fakePDSProvider {
	state := fakeProviderState{OAuthState: "dummy_state"}
	stateBytes, _ := json.Marshal(state)

	var testT *testing.T
	if len(t) > 0 {
		testT = t[0]
	}

	f := &fakePDSProvider{
		authorizeState: stateBytes,
		t:              testT,
	}

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			redirect := r.URL.Query().Get("redirect_uri")
			q := url.Values{
				"code":  []string{"dummyCode"},
				"state": []string{r.URL.Query().Get("state")},
			}
			w.Header().Add("Location", redirect+"?"+q.Encode())
			w.WriteHeader(http.StatusSeeOther)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	f.server = server
	return f
}

type fakeProviderState struct {
	OAuthState string `json:"oauth_state"`
}

var _ login.Provider = (*fakePDSProvider)(nil)

func (f *fakePDSProvider) LoginMethod() org.LoginMethod { return org.LoginMethodAtproto }

func (f *fakePDSProvider) Authorize(ctx context.Context, id *identity.Identity, loginID string) (string, []byte, error) {
	q := url.Values{}
	if f.redirectURI != "" {
		q.Set("redirect_uri", f.redirectURI)
	}
	u := f.server.URL + "/authorize"
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	return u, f.authorizeState, nil
}

func (f *fakePDSProvider) Exchange(ctx context.Context, did syntax.DID, code, issuer, oauthState string, state []byte) error {
	if f.exchangeFn != nil {
		return f.exchangeFn(ctx, did, code, issuer, oauthState, state)
	}
	if f.t != nil {
		require.Equal(f.t, "dummyCode", code)
	}
	return nil
}

func (f *fakePDSProvider) SetRedirectURI(uri string) {
	f.redirectURI = uri
}

func (f *fakePDSProvider) Close() {
	if f.server != nil {
		f.server.Close()
	}
}
