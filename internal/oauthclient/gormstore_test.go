package oauthclient

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
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
