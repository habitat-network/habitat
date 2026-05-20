package pdsclient

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) *OAuthStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	store, err := NewOAuthStore(db, encrypt.TestKey)
	require.NoError(t, err)
	return store
}

func TestOAuthStore_SaveAndGetSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	did := syntax.DID("did:plc:test123")
	sess := oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               "default",
		HostURL:                 "https://pds.example.com",
		AuthServerURL:           "https://auth.example.com",
		AuthServerTokenEndpoint: "https://auth.example.com/token",
		Scopes:                  []string{"atproto"},
		AccessToken:             "access_token_123",
		RefreshToken:            "refresh_token_456",
		DPoPPrivateKeyMultibase: "zQYgxqHxLQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQq",
		DPoPAuthServerNonce:     "nonce_abc",
		DPoPHostNonce:           "nonce_def",
	}

	err := store.SaveSession(ctx, sess)
	require.NoError(t, err)

	got, err := store.GetSession(ctx, did, "default")
	require.NoError(t, err)
	assert.Equal(t, sess.AccountDID, got.AccountDID)
	assert.Equal(t, sess.AccessToken, got.AccessToken)
	assert.Equal(t, sess.RefreshToken, got.RefreshToken)
	assert.Equal(t, sess.DPoPPrivateKeyMultibase, got.DPoPPrivateKeyMultibase)
	assert.Equal(t, sess.Scopes, got.Scopes)
}

func TestOAuthStore_DeleteSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	did := syntax.DID("did:plc:test456")
	sess := oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               "default",
		HostURL:                 "https://pds.example.com",
		AuthServerURL:           "https://auth.example.com",
		AccessToken:             "token",
		RefreshToken:            "refresh",
		DPoPPrivateKeyMultibase: "ztestkey",
	}
	require.NoError(t, store.SaveSession(ctx, sess))
	require.NoError(t, store.DeleteSession(ctx, did, "default"))
	_, err := store.GetSession(ctx, did, "default")
	assert.Error(t, err)
}

func TestOAuthStore_AuthRequestInfo(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	info := oauth.AuthRequestData{
		State:         "state_123",
		AuthServerURL: "https://auth.example.com",
		Scopes:        []string{"atproto"},
		RequestURI:    "urn:ietf:params:oauth:request_uri:abc",
		PKCEVerifier:  "verifier_123",
	}

	err := store.SaveAuthRequestInfo(ctx, info)
	require.NoError(t, err)

	got, err := store.GetAuthRequestInfo(ctx, "state_123")
	require.NoError(t, err)
	assert.Equal(t, info.State, got.State)
	assert.Equal(t, info.RequestURI, got.RequestURI)
	assert.Equal(t, info.PKCEVerifier, got.PKCEVerifier)

	require.NoError(t, store.DeleteAuthRequestInfo(ctx, "state_123"))
	_, err = store.GetAuthRequestInfo(ctx, "state_123")
	assert.Error(t, err)
}

func TestOAuthStore_GetMissingSession(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	_, err := store.GetSession(ctx, syntax.DID("did:plc:nonexistent"), "default")
	assert.Error(t, err)
}

func TestOAuthStore_EncryptionRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	did := syntax.DID("did:plc:encrypt_test")
	orig := oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               "default",
		HostURL:                 "https://pds.example.com",
		AuthServerURL:           "https://auth.example.com",
		AccessToken:             "secret_token_abc",
		RefreshToken:            "secret_refresh_xyz",
		DPoPPrivateKeyMultibase: "zQYgxqHxLQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQqQq",
	}
	require.NoError(t, store.SaveSession(ctx, orig))
	got, err := store.GetSession(ctx, did, "default")
	require.NoError(t, err)
	assert.Equal(t, orig.AccessToken, got.AccessToken)
	assert.Equal(t, orig.RefreshToken, got.RefreshToken)
	assert.Equal(t, orig.DPoPPrivateKeyMultibase, got.DPoPPrivateKeyMultibase)
}
