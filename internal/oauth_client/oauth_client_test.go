package oauth_client

import (
	"context"
	"fmt"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testStore struct {
	sessions     map[string]*oauth.ClientSessionData
	authRequests map[string]*oauth.AuthRequestData
}

func newTestStore() *testStore {
	return &testStore{
		sessions:     make(map[string]*oauth.ClientSessionData),
		authRequests: make(map[string]*oauth.AuthRequestData),
	}
}

func (s *testStore) GetSession(
	ctx context.Context,
	did syntax.DID,
	sessionID string,
) (*oauth.ClientSessionData, error) {
	key := string(did) + "/" + sessionID
	v, ok := s.sessions[key]
	if !ok {
		return nil, fmt.Errorf("session not found: %s", did)
	}
	return v, nil
}

func (s *testStore) SaveSession(ctx context.Context, sess oauth.ClientSessionData) error {
	key := string(sess.AccountDID) + "/" + sess.SessionID
	s.sessions[key] = &sess
	return nil
}

func (s *testStore) DeleteSession(ctx context.Context, did syntax.DID, sessionID string) error {
	delete(s.sessions, string(did)+"/"+sessionID)
	return nil
}

func (s *testStore) GetAuthRequestInfo(
	ctx context.Context,
	state string,
) (*oauth.AuthRequestData, error) {
	v, ok := s.authRequests[state]
	if !ok {
		return nil, fmt.Errorf("request info not found: %s", state)
	}
	return v, nil
}

func (s *testStore) SaveAuthRequestInfo(ctx context.Context, info oauth.AuthRequestData) error {
	s.authRequests[info.State] = &info
	return nil
}

func (s *testStore) DeleteAuthRequestInfo(ctx context.Context, state string) error {
	delete(s.authRequests, state)
	return nil
}

func testApp(store oauth.ClientAuthStore) *App {
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	return NewApp(&cfg, store)
}

func TestNewApp(t *testing.T) {
	store := newTestStore()
	app := testApp(store)
	require.NotNil(t, app)
	assert.Equal(t, "https://example.com/client-metadata.json", app.Config.ClientID)
}

func TestApp_StartAuthFlow_FailsWithoutServer(t *testing.T) {
	app := testApp(newTestStore())
	_, err := app.StartAuthFlow(context.Background(), "did:plc:nonexistent")
	assert.Error(t, err)
}

func TestApp_GetClient_FailsWithoutSession(t *testing.T) {
	app := testApp(newTestStore())
	_, err := app.GetClient(context.Background(), syntax.DID("did:plc:nonexistent"), "sess1")
	assert.Error(t, err)
}

func TestApp_ProcessCallback_InvalidParams(t *testing.T) {
	app := testApp(newTestStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{})
	assert.Error(t, err)
}

func TestApp_Logout_Idempotent(t *testing.T) {
	app := testApp(newTestStore())
	err := app.Logout(context.Background(), syntax.DID("did:plc:nonexistent"), "sess1")
	assert.NoError(t, err)
}

func TestApp_ProcessCallback_MissingState(t *testing.T) {
	app := testApp(newTestStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{"code": {"abc"}})
	assert.Error(t, err)
}

func TestApp_ProcessCallback_MissingCode(t *testing.T) {
	app := testApp(newTestStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{"state": {"abc"}})
	assert.Error(t, err)
}

func TestApp_ProcessCallback_StateNotFound(t *testing.T) {
	app := testApp(newTestStore())
	_, err := app.ProcessCallback(context.Background(), url.Values{
		"state": {"nonexistent"},
		"code":  {"abc"},
	})
	assert.Error(t, err)
}
