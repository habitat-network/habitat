package pearserver_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/habitat-network/habitat/internal/utils"
)

// newOpenSocialServer returns a TestServer authenticating did as an org member
// reaching the ODS through Atproto-Proxy, i.e. via a service-auth token.
func newOpenSocialServer(t *testing.T, did syntax.DID) *pearserver_testutil.TestServer {
	t.Helper()
	return newOpenSocialServerAs(t, nil, did, authn.ValidatorMethodServiceAuth)
}

// newOpenSocialServerAs returns a TestServer authenticating did with the given
// auth method, for the endpoints that treat the two differently. When ts is
// non-nil the returned server shares ts's backing stores, so state written
// through either is visible to the other; otherwise it gets fresh ones.
func newOpenSocialServerAs(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	did syntax.DID,
	method authn.ValidatorMethod,
) *pearserver_testutil.TestServer {
	t.Helper()
	opts := []utils.Opt[pearserver_testutil.TestServer]{
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(
				&authn.CredentialInfo{Subject: did, Method: method},
			),
		),
	}
	if ts != nil {
		opts = append(
			opts,
			pearserver_testutil.WithDB(ts.DB),
			pearserver_testutil.WithFGA(ts.FGA),
			pearserver_testutil.WithSpaceStore(ts.SpaceStore),
			pearserver_testutil.WithNangoClient(ts.NangoClient),
		)
	}
	return pearserver_testutil.NewTestServer(t, opts...)
}

// newSharedOpenSocialServers returns two TestServers (authenticated as admin
// and alice) that share the same backing stores, so invites created by the
// admin are visible to alice.
func newSharedOpenSocialServers(
	t *testing.T,
) (*pearserver_testutil.TestServer, *pearserver_testutil.TestServer, spaces.Store) {
	t.Helper()
	db := db_testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))
	nangoClient := pearserver_testutil.NewFakeNangoClient()

	adminTS := pearserver_testutil.NewTestServer(
		t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{
				Subject: admin,
				Method:  authn.ValidatorMethodServiceAuth,
			}),
		),
		pearserver_testutil.WithDB(db),
		pearserver_testutil.WithFGA(fga),
		pearserver_testutil.WithSpaceStore(sp),
		pearserver_testutil.WithNangoClient(nangoClient),
	)
	aliceTS := pearserver_testutil.NewTestServer(
		t,
		pearserver_testutil.WithValidator(
			authntest.NewSuccessValidator(&authn.CredentialInfo{
				Subject: alice,
				Method:  authn.ValidatorMethodServiceAuth,
			}),
		),
		pearserver_testutil.WithDB(db),
		pearserver_testutil.WithFGA(fga),
		pearserver_testutil.WithSpaceStore(sp),
		pearserver_testutil.WithNangoClient(nangoClient),
	)
	return adminTS, aliceTS, sp
}

// bootstrapAdminMemberships creates the org's members space with `admin`
// holding the admin role, mirroring the subset of NewOrg's setup the invite
// flow depends on.
func bootstrapAdminMemberships(t *testing.T, sp spaces.Store) {
	t.Helper()
	membersSpace, err := sp.CreateSpace(
		t.Context(), org, "community.opensocial.members", "self",
	)
	require.NoError(t, err)
	_, _, err = sp.PutRecord(
		t.Context(), membersSpace, org, "community.opensocial.membership",
		syntax.RecordKey(admin),
		spaces_testutil.MustMarshalRecord(
			t,
			opensocial_api.CommunityOpensocialMembership{Roles: []string{opensocial.AdminRoleRkey}},
		),
	)
	require.NoError(t, err)
}
