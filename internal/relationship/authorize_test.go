package relationship

import (
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/authn"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/perms"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Test_authorizeCanWrite exercises authorizeCanWrite directly: it is an
// unexported method, and the cases below check its ownership logic in
// isolation from any HTTP request or the validator, so this stays a unit test
// against the store rather than a routed one.
func Test_authorizeCanWrite(t *testing.T) {
	testOrg := syntax.DID("did:plc:org")
	alice := syntax.DID("did:plc:alice")

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	db := db_testutil.NewDB(t)
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))
	ps := perms.NewStore(db, sp, fga)

	space, err := sp.CreateSpace(
		t.Context(), testOrg, testOrg, syntax.NSID("network.habitat.docs"), habitat_syntax.SpaceKey("doc"),
	)
	require.NoError(t, err)

	// authorizeCanWrite never consults the validator, so nil is fine here.
	s := NewServer(ps, sp, nil)

	_, err = ps.SetUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleManager)
	require.NoError(t, err)

	t.Run("caller is not space owner but subject is", func(t *testing.T) {
		require.False(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: alice},
			true,
			space,
			habitat_syntax.SpaceRoleReader,
		))
	})
	t.Run("caller is not space owner but role is", func(t *testing.T) {
		require.False(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: alice},
			false,
			space,
			habitat_syntax.SpaceRoleOwner,
		))
	})
	t.Run("caller is space owner but subject isn't", func(t *testing.T) {
		require.True(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: testOrg},
			false,
			space,
			habitat_syntax.SpaceRoleOwner,
		))
	})
	t.Run("caller and subject are space owner", func(t *testing.T) {
		require.True(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: testOrg},
			true,
			space,
			habitat_syntax.SpaceRoleReader,
		))
	})
	t.Run("caller and subject are not space owner", func(t *testing.T) {
		require.True(t, s.authorizeCanWrite(
			t.Context(),
			httptest.NewRecorder(),
			&authn.CredentialInfo{Subject: alice},
			false,
			space,
			habitat_syntax.SpaceRoleReader,
		))
	})
}
