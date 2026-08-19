package authn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/perms"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestDelegationAuthMethod(t *testing.T) {
	userKey, _ := atcrypto.GeneratePrivateKeyK256()
	userPubKey, _ := userKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(*did.Web("pear.com").AtprotoKey(userPubKey.Multibase()).Build())
	// Owned by a different DID than the acting user below, so that
	// perms.Store's implicit space-owner grant doesn't mask the
	// "no permission" cases.
	space := habitat_syntax.SpaceURI("at://did:web:space-owner.com/space/com.test.space/abc")
	user := syntax.DID("did:web:pear.com")

	t.Run("can handle", func(t *testing.T) {
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		require.True(t, authn.NewDelegationTokenAuthMethod(dir, nil, nil).CanHandle(r))
	})

	t.Run("has permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		require.NoError(t, fga.Write(
			t.Context(),
			fgastore.MemberUserString(user),
			fgastore.RelationSpaceReader,
			fgastore.SpaceObjectKey(space),
		))
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		credInfo, ok := authn.NewDelegationTokenAuthMethod(
			dir,
			perms.NewStore(fga),
			nil,
		).Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, credInfo, &authn.CredentialInfo{Space: space})
	})

	t.Run("no permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		token, err := utils.DelegationToken(userKey, user, "#atproto", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		_, ok := authn.NewDelegationTokenAuthMethod(
			dir,
			perms.NewStore(fga),
			nil,
		).Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
	})

	t.Run("host key has permission", func(t *testing.T) {
		hostKey, _ := atcrypto.GeneratePrivateKeyK256()
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		require.NoError(t, fga.Write(
			t.Context(),
			fgastore.MemberUserString(user),
			fgastore.RelationSpaceReader,
			fgastore.SpaceObjectKey(space),
		))
		token, err := utils.DelegationToken(hostKey, user, "#habitat", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		credInfo, ok := authn.NewDelegationTokenAuthMethod(
			dir,
			perms.NewStore(fga),
			hostKey,
		).Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, credInfo, &authn.CredentialInfo{Space: space})
	})

	t.Run("host key no permission", func(t *testing.T) {
		hostKey, _ := atcrypto.GeneratePrivateKeyK256()
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		token, err := utils.DelegationToken(hostKey, user, "#habitat", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		_, ok := authn.NewDelegationTokenAuthMethod(
			dir,
			perms.NewStore(fga),
			hostKey,
		).Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
	})

	t.Run("no token", func(t *testing.T) {
		_, ok := authn.NewDelegationTokenAuthMethod(dir, nil, nil).Validate(
			httptest.NewRecorder(),
			httptest.NewRequest("GET", "/", http.NoBody),
		)
		require.False(t, ok)
	})

	t.Run("invalid signature", func(t *testing.T) {
		otherKey, _ := atcrypto.GeneratePrivateKeyK256()
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		require.NoError(t, fga.Write(
			t.Context(),
			fgastore.MemberUserString(user),
			fgastore.RelationSpaceReader,
			fgastore.SpaceObjectKey(space),
		))
		token, err := utils.DelegationToken(otherKey, user, "#atproto", space)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		credInfo, ok := authn.NewDelegationTokenAuthMethod(
			dir,
			perms.NewStore(fga),
			nil,
		).Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})
}
