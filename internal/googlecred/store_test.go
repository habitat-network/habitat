package googlecred

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) GoogleCredentialStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	s, err := NewGoogleCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	return s
}

func TestGoogleCredentialStore_UpsertAndGet(t *testing.T) {
	s := newTestStore(t)
	did := syntax.DID("did:web:example.com")

	creds := &Credentials{
		AccessToken:  "ya29.a0AfH6SMC...",
		RefreshToken: "1//0gABCDEF...",
		Expiry:       time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		IDToken:      "eyJhbGciOiJSUzI1NiIs...",
		Email:        "user@gmail.com",
	}

	err := s.UpsertCredentials(t.Context(), did, creds)
	require.NoError(t, err)

	got, err := s.GetCredentials(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, creds.AccessToken, got.AccessToken)
	require.Equal(t, creds.RefreshToken, got.RefreshToken)
	require.WithinDuration(t, creds.Expiry, got.Expiry, 0)
	require.Equal(t, creds.IDToken, got.IDToken)
	require.Equal(t, creds.Email, got.Email)
}

func TestGoogleCredentialStore_UpdateExisting(t *testing.T) {
	s := newTestStore(t)
	did := syntax.DID("did:web:example.com")

	creds := &Credentials{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       time.Now(),
		IDToken:      "old-id",
		Email:        "old@example.com",
	}
	require.NoError(t, s.UpsertCredentials(t.Context(), did, creds))

	creds.AccessToken = "new-access"
	creds.RefreshToken = "new-refresh"
	require.NoError(t, s.UpsertCredentials(t.Context(), did, creds))

	got, err := s.GetCredentials(t.Context(), did)
	require.NoError(t, err)
	require.Equal(t, "new-access", got.AccessToken)
	require.Equal(t, "new-refresh", got.RefreshToken)
	require.Equal(t, "old@example.com", got.Email)
}

func TestGoogleCredentialStore_NotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetCredentials(t.Context(), syntax.DID("did:web:nonexistent"))
	require.Error(t, err)
}
