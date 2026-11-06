package mcpgateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

// tokenEndpoint serves status and body for every token request, recording
// the last request's form and basic auth.
func tokenEndpoint(t *testing.T, status int, body string) (string, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		last = *r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &last
}

func TestOAuthConfigRefresh(t *testing.T) {
	ok := func(fields map[string]any) string {
		data, err := json.Marshal(fields)
		require.NoError(t, err)
		return string(data)
	}

	t.Run("sends the resource and client in the body", func(t *testing.T) {
		tokenURL, last := tokenEndpoint(t, http.StatusOK, ok(map[string]any{
			"access_token": "at2", "token_type": "Bearer", "refresh_token": "rt2", "expires_in": 60,
		}))
		cfg := &oauthConfig{
			Resource: "https://mcp.example.com/mcp", TokenURL: tokenURL,
			ClientID: "id", ClientSecret: "secret", AuthStyle: oauth2.AuthStyleInParams,
		}
		token, err := cfg.refresh(t.Context(), http.DefaultClient, "rt1")
		require.NoError(t, err)
		require.Equal(t, "at2", token.AccessToken)
		require.Equal(t, "rt2", token.RefreshToken)
		require.WithinDuration(t, time.Now().Add(time.Minute), token.Expiry, 5*time.Second)
		require.Equal(t, url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {"rt1"},
			"resource":      {"https://mcp.example.com/mcp"},
			"client_id":     {"id"},
			"client_secret": {"secret"},
		}, last.PostForm)
	})

	t.Run("sends the client as basic auth", func(t *testing.T) {
		tokenURL, last := tokenEndpoint(t, http.StatusOK, ok(map[string]any{"access_token": "at2"}))
		cfg := &oauthConfig{
			TokenURL: tokenURL, ClientID: "id:1", ClientSecret: "s&s",
			AuthStyle: oauth2.AuthStyleInHeader,
		}
		token, err := cfg.refresh(t.Context(), http.DefaultClient, "rt1")
		require.NoError(t, err)
		user, pass, hasBasic := last.BasicAuth()
		require.True(t, hasBasic)
		// RFC 6749 2.3.1: form-urlencoded before base64.
		require.Equal(t, url.QueryEscape("id:1"), user)
		require.Equal(t, url.QueryEscape("s&s"), pass)
		require.Empty(t, last.PostForm.Get("client_id"))
		// An unrotated refresh token is kept, and no expires_in is no expiry.
		require.Equal(t, "rt1", token.RefreshToken)
		require.True(t, token.Expiry.IsZero())
	})

	t.Run("a public client sends its id without a secret", func(t *testing.T) {
		tokenURL, last := tokenEndpoint(t, http.StatusOK, ok(map[string]any{"access_token": "at2"}))
		cfg := &oauthConfig{TokenURL: tokenURL, ClientID: "id", AuthStyle: oauth2.AuthStyleInParams}
		_, err := cfg.refresh(t.Context(), http.DefaultClient, "rt1")
		require.NoError(t, err)
		require.Equal(t, "id", last.PostForm.Get("client_id"))
		_, hasSecret := last.PostForm["client_secret"]
		require.False(t, hasSecret)
	})

	for name, tc := range map[string]struct {
		status       int
		body         string
		invalidGrant bool
	}{
		"rejected grant": {http.StatusBadRequest, `{"error":"invalid_grant"}`, true},
		"other oauth error": {
			http.StatusUnauthorized, `{"error":"invalid_client"}`, false,
		},
		"outage":             {http.StatusBadGateway, `<html>bad gateway</html>`, false},
		"no access token":    {http.StatusOK, `{"token_type":"Bearer"}`, false},
		"not json":           {http.StatusOK, `access_token=at`, false},
		"error with 200 too": {http.StatusOK, `{"error":"invalid_grant"}`, false},
	} {
		t.Run(name, func(t *testing.T) {
			tokenURL, _ := tokenEndpoint(t, tc.status, tc.body)
			cfg := &oauthConfig{TokenURL: tokenURL, ClientID: "id"}
			_, err := cfg.refresh(t.Context(), http.DefaultClient, "rt1")
			require.Error(t, err)
			// Only a rejected grant disconnects the member; anything else
			// may be transient.
			require.Equal(t, tc.invalidGrant, err == errInvalidGrant, err)
		})
	}
}
