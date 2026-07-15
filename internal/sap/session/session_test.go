package session

import (
	"crypto/rand"
	"crypto/rsa"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/oauthclient"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func testJWT(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}

func TestStoreSessionsAndSpaceAccess(t *testing.T) {
	t.Parallel()
	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))

	oauthStore, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	s := NewStore(db, oauthclient.NewApp(&cfg, oauthStore))

	require.NoError(t, oauthStore.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:  "did:plc:alice",
		SessionID:   "sess1",
		HostURL:     "https://host.example",
		AccessToken: testJWT(t),
	}))
	require.NoError(t, s.Add(t.Context(), "did:plc:alice", "sess1"))

	dids, err := s.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"did:plc:alice"}, []string{dids[0].String()})

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, s.RecordSpaceAccess(t.Context(), space, "did:plc:alice"))
	require.NoError(t, s.RecordSpaceAccess(t.Context(), space, "did:plc:alice")) // idempotent

	spaces, err := s.Spaces(t.Context())
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{space}, spaces)

	// A client is available via the recorded accessor even though the space
	// owner has no session.
	client, err := s.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	require.NotNil(t, client)

	require.NoError(t, s.DropSpace(t.Context(), space))
	spaces, err = s.Spaces(t.Context())
	require.NoError(t, err)
	require.Empty(t, spaces)
}
