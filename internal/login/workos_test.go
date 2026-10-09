package login

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func newTestWorkOS(t *testing.T, h http.HandlerFunc) Provider {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	p, err := NewWorkOSProvider("client_1", "sk_test", "https://example.com/cb", srv.URL, nil)
	require.NoError(t, err)
	return p
}

func TestWorkOSProvider_Authorize(t *testing.T) {
	p, err := NewWorkOSProvider("client_1", "sk_test", "https://example.com/cb", "", nil)
	require.NoError(t, err)
	redirect, state, providerState, err := p.Authorize(t.Context(), "alice@acme.com")
	require.NoError(t, err)
	u, err := url.Parse(redirect)
	require.NoError(t, err)
	require.Equal(t, "api.workos.com", u.Host)
	require.Equal(t, "/user_management/authorize", u.Path)
	q := u.Query()
	require.Equal(t, "client_1", q.Get("client_id"))
	require.Equal(t, "authkit", q.Get("provider"))
	require.Equal(t, "alice@acme.com", q.Get("login_hint"))
	require.Equal(t, state, q.Get("state"))
	require.NotEmpty(t, providerState)
}

func TestWorkOSProvider_Exchange(t *testing.T) {
	verified := true
	p := newTestWorkOS(t, func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "Bearer sk_test", r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/user_management/authenticate":
			var body map[string]string
			require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
			require.Equal(t, "authorization_code", body["grant_type"])
			require.Equal(t, "the-code", body["code"])
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{
				"id": "user_1", "email": "alice@acme.com", "email_verified": verified,
				"first_name": "Alice", "last_name": "A", "profile_picture_url": "https://pic",
			}})
		case "/user_management/organization_memberships":
			require.Equal(t, "user_1", r.URL.Query().Get("user_id"))
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
				{"organization_id": "org_1", "status": "active"},
				{"organization_id": "org_2", "status": "inactive"},
			}})
		default:
			http.NotFound(w, r)
		}
	})
	_, state, providerState, err := p.Authorize(t.Context(), "")
	require.NoError(t, err)

	t.Run("returns email, profile and active orgs", func(t *testing.T) {
		loginID, profile, err := p.Exchange(
			t.Context(), url.Values{"code": {"the-code"}, "state": {state}}, providerState,
		)
		require.NoError(t, err)
		require.Equal(t, "alice@acme.com", loginID)
		require.Equal(t, Profile{
			Name: "Alice A", Picture: "https://pic", ExternalOrgIDs: []string{"org_1"},
		}, profile)
	})

	t.Run("rejects state mismatch", func(t *testing.T) {
		_, _, err := p.Exchange(
			t.Context(), url.Values{"code": {"the-code"}, "state": {"bad"}}, providerState,
		)
		require.Error(t, err)
	})

	t.Run("rejects unverified email", func(t *testing.T) {
		verified = false
		defer func() { verified = true }()
		_, _, err := p.Exchange(
			t.Context(), url.Values{"code": {"the-code"}, "state": {state}}, providerState,
		)
		require.Error(t, err)
	})
}
