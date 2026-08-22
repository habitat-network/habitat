package testutil_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
)

func TestNewOrgCreatesAdmin(t *testing.T) {
	p := testutil.New(t)

	admin := p.NewOrg("acme")

	require.NotEmpty(t, admin.DID)
	require.NotEmpty(t, admin.Org)
	require.NotEmpty(t, admin.Token)

	org, err := p.OrgStore.GetOrg(t.Context(), admin.Org)
	require.NoError(t, err)
	isAdmin, err := org.IsAdmin(t.Context(), admin.DID)
	require.NoError(t, err)
	require.True(t, isAdmin, "NewOrg's actor must be an admin of the org it created")
}

func TestNewMemberJoinsOrg(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	member := p.NewMember(admin, "alice")

	require.Equal(t, admin.Org, member.Org)
	require.NotEqual(t, admin.DID, member.DID)

	org, err := p.OrgStore.GetOrg(t.Context(), admin.Org)
	require.NoError(t, err)
	isMember, err := org.IsMember(t.Context(), member.DID)
	require.NoError(t, err)
	require.True(t, isMember)

	isAdmin, err := org.IsAdmin(t.Context(), member.DID)
	require.NoError(t, err)
	require.False(t, isAdmin, "a plain member must not be an admin")
}

func TestActorTokenAuthenticates(t *testing.T) {
	p := testutil.New(t)
	admin := p.NewOrg("acme")

	credInfo, ok, err := p.OAuthServer.ValidateRaw(t.Context(), admin.Token)
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, admin.DID, credInfo.Subject)
	require.Equal(t, admin.Org, credInfo.Org.DID(), "the real validator resolves the org from membership")
}

func TestAnonymousHasNoToken(t *testing.T) {
	p := testutil.New(t)

	require.Empty(t, p.Anonymous().Token)
}
