package pdsclient

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClientApp(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	store := newTestStore(t)

	app, err := NewClientApp(ClientAppConfig{
		ClientID:    "https://app.example.com/client-metadata.json",
		CallbackURL: "https://app.example.com/oauth-callback",
		UserAgent:   "habitat/0.1",
		Scopes:      []string{"atproto", "transition:generic"},
		PrivateKey:  key,
		KeyID:       "habitat",
		Store:       store,
	})
	require.NoError(t, err)
	require.NotNil(t, app)
	assert.Equal(t, "https://app.example.com/client-metadata.json", app.Config.ClientID)
	assert.True(t, app.Config.IsConfidential())
}

func TestClientApp_ClientMetadata(t *testing.T) {
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	store := newTestStore(t)

	app, err := NewClientApp(ClientAppConfig{
		ClientID:    "https://app.example.com/client-metadata.json",
		CallbackURL: "https://app.example.com/oauth-callback",
		UserAgent:   "habitat/0.1",
		Scopes:      []string{"atproto", "transition:generic"},
		PrivateKey:  key,
		KeyID:       "habitat",
		Store:       store,
	})
	require.NoError(t, err)

	meta := app.Config.ClientMetadata()
	assert.Equal(t, "https://app.example.com/client-metadata.json", meta.ClientID)
	assert.Equal(t, "private_key_jwt", meta.TokenEndpointAuthMethod)
	assert.True(t, meta.DPoPBoundAccessTokens)
	assert.Contains(t, meta.RedirectURIs, "https://app.example.com/oauth-callback")
}
