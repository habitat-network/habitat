package opensocial_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	group_api "github.com/habitat-network/habitat/api/atgrp"
	"github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestAllowsLegacySpaceCredential(t *testing.T) {
	ctx := t.Context()
	s := testutil.NewTestStore(t)
	person := syntax.DID("did:plc:person")
	spaceType := syntax.NSID("network.habitat.group")
	staged, err := s.SpaceStore.CreateSpace(ctx, person, spaceType, "staged")
	require.NoError(t, err)
	legacy, err := s.SpaceStore.CreateSpace(ctx, person, spaceType, "legacy")
	require.NoError(t, err)
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, s.SpaceStore, staged, spaces.Policy{
		Type: spaces.PolicyAtgrp,
	}))

	group, err := s.NewCanonicalGroup(ctx, "workshop", person, group_api.OrgAtgrpAbout{
		DisplayName: "Workshop",
	})
	require.NoError(t, err)
	canonicalWithoutPolicy, err := s.SpaceStore.CreateSpace(ctx, group, spaceType, "legacy")
	require.NoError(t, err)

	for _, tc := range []struct {
		name    string
		space   habitat_syntax.SpaceURI
		allowed bool
	}{
		{"staged canonical policy", staged, false},
		{"unmarked sibling with legacy policy", legacy, true},
		{"canonical owner with legacy policy", canonicalWithoutPolicy, false},
		{"canonical owner with no local space", habitat_syntax.ConstructSpaceURI(group, spaceType, "missing"), false},
		{"remote owner with no local space", habitat_syntax.ConstructSpaceURI("did:web:remote.example", spaceType, "missing"), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			allowed, err := s.AllowsLegacySpaceCredential(t.Context(), tc.space)
			require.NoError(t, err)
			require.Equal(t, tc.allowed, allowed)
		})
	}
}

func TestAllowsLegacySpaceCredentialLookupErrors(t *testing.T) {
	ctx := t.Context()
	s := testutil.NewTestStore(t)
	uri := habitat_syntax.ConstructSpaceURI("did:plc:person", "network.habitat.group", "self")

	require.NoError(t, s.DB.Migrator().DropTable("spaces"))
	allowed, err := s.AllowsLegacySpaceCredential(ctx, uri)
	require.ErrorContains(t, err, "check space credential policy")
	require.False(t, allowed)

	require.NoError(t, s.DB.Migrator().DropTable("canonical_group_rows"))
	allowed, err = s.AllowsLegacySpaceCredential(ctx, uri)
	require.ErrorContains(t, err, "look up canonical group")
	require.False(t, allowed)
}
