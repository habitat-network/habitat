package pdsclient

import (
	"testing"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	return db
}

func TestNewClient(t *testing.T) {
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)

	client, err := NewClient(
		newTestDB(t),
		"https://app.example.com/client-metadata.json",
		"https://app.example.com",
		"https://app.example.com/oauth-callback",
		secret,
	)
	require.NoError(t, err)
	require.NotNil(t, client)

	meta := client.ClientMetadata()
	assert.Equal(t, "https://app.example.com/client-metadata.json", meta.ClientID)
	assert.True(t, meta.DPoPBoundAccessTokens)
	assert.Contains(t, meta.RedirectURIs, "https://app.example.com/oauth-callback")
}
