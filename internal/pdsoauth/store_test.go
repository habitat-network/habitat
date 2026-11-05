package pdsoauth

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/stretchr/testify/require"
)

func newTestStore(t *testing.T) (*Store, keyval.Store) {
	t.Helper()
	kv := keyval_testutil.New(t)
	enc, err := keyval.NewEncrypted(kv, encrypt.TestKey)
	require.NoError(t, err)
	return NewStore(
		enc.Namespace("sessions"),
		kv.Namespace("sessions"),
		kv.Namespace("requests"),
	), kv
}

func TestStoreSessionLifecycle(t *testing.T) {
	s, kv := newTestStore(t)
	did := testDID

	_, err := s.GetSession(t.Context(), did, "")
	require.ErrorIs(t, err, ErrSessionNotFound)

	require.NoError(t, s.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:   did,
		SessionID:    "first",
		AccessToken:  "access",
		RefreshToken: "refresh",
		Scopes:       []string{"atproto"},
	}))
	got, err := s.GetSession(t.Context(), did, "ignored")
	require.NoError(t, err)
	require.Equal(t, "first", got.SessionID)
	require.Equal(t, "refresh", got.RefreshToken)
	require.Equal(t, []string{"atproto"}, got.Scopes)

	// A second sign-in replaces the DID's session.
	require.NoError(t, s.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID: did,
		SessionID:  "second",
	}))
	got, err = s.GetSession(t.Context(), did, "")
	require.NoError(t, err)
	require.Equal(t, "second", got.SessionID)

	// Tokens aren't readable from the backing store.
	var raw map[string]any
	require.Error(t, kv.Namespace("sessions").Get(t.Context(), did.String(), &raw))

	require.NoError(t, s.DeleteSession(t.Context(), did, ""))
	_, err = s.GetSession(t.Context(), did, "")
	require.ErrorIs(t, err, ErrSessionNotFound)
	// Deleting again is not an error.
	require.NoError(t, s.DeleteSession(t.Context(), did, ""))
}

func TestStoreAuthRequestLifecycle(t *testing.T) {
	s, _ := newTestStore(t)
	info := oauth.AuthRequestData{
		State:         "state1",
		PKCEVerifier:  "verifier",
		AuthServerURL: "https://as",
	}

	_, err := s.GetAuthRequestInfo(t.Context(), "state1")
	require.ErrorIs(t, err, keyval.ErrNotFound)

	ctx, reported := WithAuthState(t.Context())
	require.NoError(t, s.SaveAuthRequestInfo(ctx, info))
	require.Equal(t, "state1", reported())
	// Create-only.
	require.Error(t, s.SaveAuthRequestInfo(t.Context(), info))

	got, err := s.GetAuthRequestInfo(t.Context(), "state1")
	require.NoError(t, err)
	require.Equal(t, "verifier", got.PKCEVerifier)

	require.NoError(t, s.DeleteAuthRequestInfo(t.Context(), "state1"))
	require.NoError(t, s.DeleteAuthRequestInfo(t.Context(), "state1"))
	_, err = s.GetAuthRequestInfo(t.Context(), "state1")
	require.ErrorIs(t, err, keyval.ErrNotFound)
}
