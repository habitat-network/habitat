package oauthclient

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testApp(t *testing.T, store oauth.ClientAuthStore) *App {
	testServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/oauth/token", r.URL.Path)
		require.NoError(t, r.ParseForm())
		require.Equal(t, "abc", r.PostForm.Get("code"))
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"access_token":  newJwtToken(t, "did:web:test.com", time.Now().Add(time.Hour)),
			"refresh_token": "refresh-token",
		}))
	}))
	t.Cleanup(testServer.Close)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	return NewApp(
		&cfg,
		store,
		WithDirectory(
			pdsclient.NewDummyDirectory(
				"pds.com",
				pdsclient.WithHabitatService(testServer.URL),
			),
		),
	)
}

func TestNewApp(t *testing.T) {
	store := oauth.NewMemStore()
	app := testApp(t, store)
	require.NotNil(t, app)
	assert.Equal(t, "https://example.com/client-metadata.json", app.Config.ClientID)
}

func TestApp(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	redirectURI, err := app.StartAuthFlow(context.Background(), "did:web:test.com")
	require.NoError(t, err)
	parsedURL, err := url.Parse(redirectURI)
	require.NoError(t, err)
	require.Equal(t, "https://example.com/oauth-callback", parsedURL.Query().Get("redirect_uri"))
	state := parsedURL.Query().Get("state")
	sess, err := app.ProcessCallback(
		context.Background(),
		url.Values{"state": {state}, "code": {"abc"}},
	)
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:web:test.com"), sess.AccountDID)
}

func TestApp_GetClient_FailsWithoutSession(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	_, err := app.GetClient(context.Background(), syntax.DID("did:plc:nonexistent"), "sess1")
	assert.Error(t, err)
}

func TestApp_ProcessCallback_InvalidParams(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{})
	assert.Error(t, err)
}

func TestApp_Logout_Idempotent(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	err := app.Logout(context.Background(), syntax.DID("did:plc:nonexistent"), "sess1")
	assert.NoError(t, err)
}

func TestApp_ProcessCallback_MissingState(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{"code": {"abc"}})
	assert.Error(t, err)
}

func TestApp_ProcessCallback_MissingCode(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{"state": {"abc"}})
	assert.Error(t, err)
}

func TestApp_ProcessCallback_StateNotFound(t *testing.T) {
	app := testApp(t, oauth.NewMemStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{
		"state": {"nonexistent"},
		"code":  {"abc"},
	})
	assert.Error(t, err)
}
