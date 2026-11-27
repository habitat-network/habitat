package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_CreateOpensocialSpace(t *testing.T) {
	t.Run("requires membership", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		client := httpx_testutil.NewTestXRPCClient(t)
		var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
		code := client.Procedure(
			ts.Server.CreateOpensocialSpace,
			habitat.NetworkHabitatOpensocialCreateSpaceInput{
				Org:  orgDID,
				Type: "network.habitat.docs",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("creates a space with the given roles", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		client := httpx_testutil.NewTestXRPCClient(t)
		var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
		code := client.Procedure(
			ts.Server.CreateOpensocialSpace,
			habitat.NetworkHabitatOpensocialCreateSpaceInput{
				Org:   orgDID,
				Type:  "network.habitat.docs",
				Roles: []string{"admin", "member"},
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Uri)

		spaceURI, err := habitat_syntax.ParseSpaceURI(out.Uri)
		require.NoError(t, err)

		allowed, err := ts.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.False(t, allowed, "alice holds no role in this org yet")

		require.NoError(t, ts.OpenSocialStore.AssignRoles(
			t.Context(), syntax.DID(orgDID), alice, []string{"member"},
		))
		allowed, err = ts.OpenSocialStore.CheckPermission(t.Context(), alice, spaceURI)
		require.NoError(t, err)
		require.True(t, allowed, "member role should be able to read the new space")
	})

	t.Run("rejects a duplicate skey", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		client := httpx_testutil.NewTestXRPCClient(t)
		input := habitat.NetworkHabitatOpensocialCreateSpaceInput{
			Org:  orgDID,
			Type: "network.habitat.docs",
			Skey: "fixed",
		}
		var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
		code := client.Procedure(ts.Server.CreateOpensocialSpace, input, &out)
		require.Equal(t, http.StatusOK, code)

		code = client.Procedure(ts.Server.CreateOpensocialSpace, input, &out)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("accepts the org's own OAuth credential", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		// An admin-approved app holds an OAuth credential for the org's
		// identity itself, not for any member's.
		orgTS := newOpenSocialServerAs(
			t, ts, syntax.DID(orgDID), authn.ValidatorMethodOAuth,
		)

		client := httpx_testutil.NewTestXRPCClient(t)
		var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
		code := client.Procedure(
			orgTS.Server.CreateOpensocialSpace,
			habitat.NetworkHabitatOpensocialCreateSpaceInput{
				Org:  orgDID,
				Type: "network.habitat.docs",
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Uri)
	})

	t.Run("rejects a member's OAuth credential", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		// admin is an admin of this org, but reached it over OAuth on its own
		// session rather than through service auth. Org spaces come from the
		// org's credential or a member's service-auth token, so this is denied.
		aliceTS := newOpenSocialServerAs(t, ts, admin, authn.ValidatorMethodOAuth)

		client := httpx_testutil.NewTestXRPCClient(t)
		var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
		code := client.Procedure(
			aliceTS.Server.CreateOpensocialSpace,
			habitat.NetworkHabitatOpensocialCreateSpaceInput{
				Org:  orgDID,
				Type: "network.habitat.docs",
			},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("stores the requested policy, defaulting to relationspace", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		client := httpx_testutil.NewTestXRPCClient(t)

		create := func(policy any) (int, habitat_syntax.SpaceURI) {
			var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
			code := client.Procedure(
				ts.Server.CreateOpensocialSpace,
				habitat.NetworkHabitatOpensocialCreateSpaceInput{
					Org: orgDID, Type: "network.habitat.docs", Policy: policy,
				},
				&out,
			)
			return code, habitat_syntax.SpaceURI(out.Uri).Canonical()
		}

		code, uri := create(nil)
		require.Equal(t, http.StatusOK, code)
		got, err := ts.SpaceStore.GetSpacePolicy(t.Context(), uri)
		require.NoError(t, err)
		require.Equal(t, spaces.Policy{Type: spaces.PolicyRelationSpace}, got)

		code, uri = create(map[string]any{"$type": spaces.PolicyPersonalSpace})
		require.Equal(t, http.StatusOK, code)
		got, err = ts.SpaceStore.GetSpacePolicy(t.Context(), uri)
		require.NoError(t, err)
		require.Equal(t, spaces.Policy{Type: spaces.PolicyPersonalSpace}, got)

		code, _ = create(map[string]any{"$type": "com.atproto.managedspace.policy"})
		require.Equal(t, http.StatusBadRequest, code)
		code, _ = create(map[string]any{"publicRead": true})
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("rejects an invalid skey", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)
		client := httpx_testutil.NewTestXRPCClient(t)
		var out habitat.NetworkHabitatOpensocialCreateSpaceOutput
		code := client.Procedure(
			ts.Server.CreateOpensocialSpace,
			habitat.NetworkHabitatOpensocialCreateSpaceInput{
				Org: orgDID, Type: "network.habitat.docs", Skey: "bad/key",
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
