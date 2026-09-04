package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_CreateOrg(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)

	t.Run("creates org and makes caller admin", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)

		var out habitat.NetworkHabitatOpensocialCreateOrgOutput
		code := client.Procedure(
			ts.Server.CreateOrg,
			habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Org)

		roles, err := ts.OpenSocialStore.GetUserRoles(t.Context(), syntax.DID(out.Org), alice)
		require.NoError(t, err)
		require.Equal(t, []string{opensocial.AdminRoleRkey}, roles)
	})

	t.Run("requires auth", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(
			t,
			pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
		)

		var out habitat.NetworkHabitatOpensocialCreateOrgOutput
		code := client.Procedure(
			ts.Server.CreateOrg,
			habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: "acme"},
			&out,
		)
		require.Equal(t, http.StatusUnauthorized, code)
	})

	t.Run("rejects invalid handles", func(t *testing.T) {
		tests := []struct {
			name   string
			handle string
		}{
			{"empty", ""},
			{"subdomain", "sub.acme"},
			{"invalid characters", "not a valid handle!"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ts := newOpenSocialServer(t, alice)

				var out habitat.NetworkHabitatOpensocialCreateOrgOutput
				code := client.Procedure(
					ts.Server.CreateOrg,
					habitat.NetworkHabitatOpensocialCreateOrgInput{Handle: tt.handle},
					&out,
				)
				require.Equal(t, http.StatusBadRequest, code)
			})
		}
	})
}
