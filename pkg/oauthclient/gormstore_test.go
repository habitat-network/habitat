package oauthclient

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestStore_SaveAndGetSession(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)

	sess := oauth.ClientSessionData{
		AccountDID: "did:plc:test",
		SessionID:  "sess1",
	}
	require.NoError(t, store.SaveSession(context.Background(), sess))

	got, err := store.GetSession(context.Background(), "did:plc:test", "sess1")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "did:plc:test", got.AccountDID.String())
	require.Equal(t, "sess1", got.SessionID)
}

func TestStore_SaveAndGetAuthRequest(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)

	info := oauth.AuthRequestData{
		State: "state123",
	}
	require.NoError(t, store.SaveAuthRequestInfo(context.Background(), info))

	got, err := store.GetAuthRequestInfo(context.Background(), "state123")
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "state123", got.State)
}

func TestStore_DeleteSession(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)

	sess := oauth.ClientSessionData{
		AccountDID: "did:plc:test",
		SessionID:  "sess1",
	}
	require.NoError(t, store.SaveSession(context.Background(), sess))

	require.NoError(t, store.DeleteSession(context.Background(), "did:plc:test", "sess1"))

	_, err = store.GetSession(context.Background(), "did:plc:test", "sess1")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestStore_DeleteAuthRequest(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)

	info := oauth.AuthRequestData{State: "state456"}
	require.NoError(t, store.SaveAuthRequestInfo(context.Background(), info))
	require.NoError(t, store.DeleteAuthRequestInfo(context.Background(), "state456"))

	_, err = store.GetAuthRequestInfo(context.Background(), "state456")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestStore_UpdateExistingSession(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)

	sess := oauth.ClientSessionData{
		AccountDID: "did:plc:test",
		SessionID:  "sess1",
		Scopes:     []string{"read"},
	}
	require.NoError(t, store.SaveSession(context.Background(), sess))

	sess.Scopes = []string{"read", "write"}
	require.NoError(t, store.SaveSession(context.Background(), sess))

	got, err := store.GetSession(context.Background(), "did:plc:test", "sess1")
	require.NoError(t, err)
	require.Equal(t, []string{"read", "write"}, got.Scopes)
}

func TestStore_MultipleSessionsPerDID(t *testing.T) {
	// Default configuration: distinct session IDs for the same DID coexist.
	store, err := NewGormStore(testutil.NewDB(t))
	require.NoError(t, err)

	did := syntax.DID("did:plc:test")
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID: did,
		SessionID:  "sess1",
	}))
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID: did,
		SessionID:  "sess2",
	}))

	got1, err := store.GetSession(context.Background(), did, "sess1")
	require.NoError(t, err)
	require.Equal(t, "sess1", got1.SessionID)

	got2, err := store.GetSession(context.Background(), did, "sess2")
	require.NoError(t, err)
	require.Equal(t, "sess2", got2.SessionID)
}

func TestStore_SingleSessionPerDID(t *testing.T) {
	db := testutil.NewDB(t)
	store, err := NewGormStore(db, WithSingleSessionPerDID())
	require.NoError(t, err)

	did := syntax.DID("did:plc:test")
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID: did,
		SessionID:  "sess1",
	}))

	// A new session under a different session ID replaces the old one.
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID: did,
		SessionID:  "sess2",
		Scopes:     []string{"read"},
	}))

	// GetSession ignores the sessionID argument and resolves the sole
	// session stored for the DID, regardless of what's passed.
	got, err := store.GetSession(context.Background(), did, "sess1")
	require.NoError(t, err)
	require.Equal(t, "sess2", got.SessionID)
	require.Equal(t, []string{"read"}, got.Scopes)

	// The old session row is gone, not just shadowed: exactly one row
	// remains for this DID.
	var count int64
	require.NoError(t, db.Model(&sessionRow{}).Where("did = ?", did.String()).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestStore_SingleSessionPerDID_DeleteIgnoresSessionID(t *testing.T) {
	store, err := NewGormStore(testutil.NewDB(t), WithSingleSessionPerDID())
	require.NoError(t, err)

	did := syntax.DID("did:plc:test")
	require.NoError(t, store.SaveSession(context.Background(), oauth.ClientSessionData{
		AccountDID: did,
		SessionID:  "sess1",
	}))

	require.NoError(t, store.DeleteSession(context.Background(), did, "irrelevant"))

	_, err = store.GetSession(context.Background(), did, "irrelevant")
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}
