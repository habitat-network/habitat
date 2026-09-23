package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/emaildomain"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_CreateEmailDomainOrg(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	create := func(
		ts *pearserver_testutil.TestServer,
		handle, domain string,
	) (habitat.NetworkHabitatEmaildomainCreateOrgOutput, int) {
		var out habitat.NetworkHabitatEmaildomainCreateOrgOutput
		code := client.Procedure(
			ts.Server.CreateEmailDomainOrg,
			habitat.NetworkHabitatEmaildomainCreateOrgInput{Handle: handle, Domain: domain},
			&out,
		)
		return out, code
	}

	t.Run("creates org mapped to domain with no members", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		out, code := create(ts, "acme", "ACME.com")
		require.Equal(t, http.StatusOK, code)
		org := syntax.DID(out.Org)

		isOrg, err := ts.OpenSocialStore.IsOrg(t.Context(), org)
		require.NoError(t, err)
		require.True(t, isOrg)

		// The caller is not made a member; the first email sign-in will be admin.
		roles, err := ts.OpenSocialStore.GetUserRoles(t.Context(), org, alice)
		require.NoError(t, err)
		require.Empty(t, roles)

		mapped, method, ok, err := ts.EmailDomainStore.LookupDomain(t.Context(), "acme.com")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, org, mapped)
		require.Equal(t, emaildomain.LoginMethodGoogle, method)
	})

	t.Run("rejects an already-mapped domain", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		_, code := create(ts, "acme", "acme.com")
		require.Equal(t, http.StatusOK, code)
		_, code = create(ts, "acme2", "acme.com")
		require.Equal(t, http.StatusConflict, code)
	})

	t.Run("does not require auth", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(
			t,
			pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
		)
		_, code := create(ts, "acme", "acme.com")
		require.Equal(t, http.StatusOK, code)
	})

	t.Run("rejects invalid input", func(t *testing.T) {
		tests := []struct {
			name, handle, domain string
		}{
			{"empty handle", "", "acme.com"},
			{"dotted handle", "sub.acme", "acme.com"},
			{"empty domain", "acme", ""},
			{"single-label domain", "acme", "localhost"},
			{"invalid domain", "acme", "not a domain!"},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				ts := newOpenSocialServer(t, alice)
				_, code := create(ts, tt.handle, tt.domain)
				require.Equal(t, http.StatusBadRequest, code)
			})
		}
	})
}
