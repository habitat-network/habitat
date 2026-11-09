package mcpgateway

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
)

var testGrant = grant{
	org: "did:web:org.example.com", server: "linear", generation: "g1", did: "did:plc:alice",
}

func newTestVault(t *testing.T) *tokenVault {
	t.Helper()
	return newTokenVault(keyval_testutil.NewEncrypted(t), http.DefaultClient)
}

func TestTokenVault(t *testing.T) {
	v := newTestVault(t)
	_, err := v.get(t.Context(), testGrant)
	require.ErrorIs(t, err, ErrNotConnected)
	require.NoError(t, v.delete(t.Context(), testGrant), "deleting nothing is no error")

	require.NoError(t, v.put(t.Context(), testGrant, &oauth2.Token{
		AccessToken: "at", TokenType: "Bearer", RefreshToken: "rt",
	}))
	got, err := v.get(t.Context(), testGrant)
	require.NoError(t, err)
	require.Equal(t, &storedToken{
		Generation: "g1", AccessToken: "at", TokenType: "Bearer", RefreshToken: "rt",
	}, got)

	// Tokens are per member, and per incarnation of the server.
	other := testGrant
	other.did = "did:plc:bob"
	_, err = v.get(t.Context(), other)
	require.ErrorIs(t, err, ErrNotConnected)
	readded := testGrant
	readded.generation = "g2"
	_, err = v.get(t.Context(), readded)
	require.ErrorIs(t, err, ErrNotConnected)

	require.NoError(t, v.delete(t.Context(), testGrant))
	_, err = v.get(t.Context(), testGrant)
	require.ErrorIs(t, err, ErrNotConnected)
}

func TestTokenVaultRefresh(t *testing.T) {
	expiring := &oauth2.Token{
		AccessToken: "at1", RefreshToken: "rt1", Expiry: time.Now().Add(time.Second),
	}

	t.Run("stores the rotated tokens", func(t *testing.T) {
		v := newTestVault(t)
		require.NoError(t, v.put(t.Context(), testGrant, expiring))
		tokenURL, last := tokenEndpoint(t, http.StatusOK,
			`{"access_token":"at2","token_type":"Bearer","refresh_token":"rt2","expires_in":3600}`)

		got, err := v.refresh(t.Context(), testGrant, &oauthConfig{TokenURL: tokenURL})
		require.NoError(t, err)
		require.Equal(t, "at2", got.AccessToken)
		require.Equal(t, "rt1", last.PostForm.Get("refresh_token"))
		stored, err := v.get(t.Context(), testGrant)
		require.NoError(t, err)
		require.Equal(t, "rt2", stored.RefreshToken)
		require.False(t, stored.needsRefresh())
	})

	t.Run("leaves fresh tokens alone", func(t *testing.T) {
		v := newTestVault(t)
		require.NoError(t, v.put(t.Context(), testGrant, &oauth2.Token{AccessToken: "at"}))
		// No token endpoint: a request to it would fail.
		got, err := v.refresh(t.Context(), testGrant, &oauthConfig{})
		require.NoError(t, err)
		require.Equal(t, "at", got.AccessToken)
	})

	t.Run("a rejected refresh token disconnects", func(t *testing.T) {
		v := newTestVault(t)
		require.NoError(t, v.put(t.Context(), testGrant, expiring))
		tokenURL, _ := tokenEndpoint(t, http.StatusBadRequest, `{"error":"invalid_grant"}`)

		_, err := v.refresh(t.Context(), testGrant, &oauthConfig{TokenURL: tokenURL})
		require.ErrorIs(t, err, ErrNotConnected)
		_, err = v.get(t.Context(), testGrant)
		require.ErrorIs(t, err, ErrNotConnected)
	})

	t.Run("an outage keeps the tokens", func(t *testing.T) {
		v := newTestVault(t)
		require.NoError(t, v.put(t.Context(), testGrant, expiring))
		tokenURL, _ := tokenEndpoint(t, http.StatusServiceUnavailable, `{}`)

		_, err := v.refresh(t.Context(), testGrant, &oauthConfig{TokenURL: tokenURL})
		require.Error(t, err)
		require.NotErrorIs(t, err, ErrNotConnected)
		_, err = v.get(t.Context(), testGrant)
		require.NoError(t, err)
	})

	t.Run("not connected", func(t *testing.T) {
		_, err := newTestVault(t).refresh(t.Context(), testGrant, &oauthConfig{})
		require.ErrorIs(t, err, ErrNotConnected)
	})
}
