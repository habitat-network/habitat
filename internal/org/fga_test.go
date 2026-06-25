package org

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/fgastore"
)

func newTestFGA(t *testing.T) fgastore.Store {
	t.Helper()
	f, err := fgastore.NewSQLite(t.Context(), t.TempDir()+"/fga.db")
	require.NoError(t, err)
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// checkOrgRole is a convenience around fga.Check for an org relation.
func checkOrgRole(t *testing.T, fga fgastore.Store, did, org syntax.DID, relation string) bool {
	t.Helper()
	ok, err := fga.Check(
		t.Context(),
		fgastore.MemberUserString(did),
		relation,
		fgastore.OrgObjectKey(org),
	)
	require.NoError(t, err)
	return ok
}

func TestCreateOrgMirrorsAdminIntoFGA(t *testing.T) {
	fga := newTestFGA(t)
	s := newTestStoreWithFGA(t, fga)

	orgID, adminID, err := s.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"sub",
	)
	require.NoError(t, err)

	require.True(t, checkOrgRole(t, fga, adminID.DID, orgID.DID, fgastore.RelationAdmin),
		"bootstrap admin should have the admin relation")
	require.True(t, checkOrgRole(t, fga, adminID.DID, orgID.DID, fgastore.RelationMember),
		"bootstrap admin should resolve as member via the union")
}

func TestOrgRoleTransitionsMirrorIntoFGA(t *testing.T) {
	fga := newTestFGA(t)
	s := newTestStoreWithFGA(t, fga)

	orgID, _, err := s.CreateOrg(
		t.Context(),
		"test-org",
		"admin",
		"password",
		"password",
		"",
		"sub",
	)
	require.NoError(t, err)

	o, err := s.GetOrg(t.Context(), orgID.DID)
	require.NoError(t, err)
	impl := o.(*orgImpl)

	// Seed a plain member directly, mirroring it into FGA as a member would be.
	memberDID := syntax.DID("did:plc:member1")
	require.NoError(t, impl.db.Create(&member{
		OrgID: orgID.DID, Did: memberDID, Role: MemberRole, LoginID: memberDID.String(),
	}).Error)
	require.NoError(t, setOrgRoleFGA(t.Context(), fga, orgID.DID, memberDID, MemberRole))

	require.True(t, checkOrgRole(t, fga, memberDID, orgID.DID, fgastore.RelationMember))
	require.False(t, checkOrgRole(t, fga, memberDID, orgID.DID, fgastore.RelationAdmin))

	// Promote to admin.
	require.NoError(t, impl.AddAdmin(t.Context(), memberDID))
	require.True(t, checkOrgRole(t, fga, memberDID, orgID.DID, fgastore.RelationAdmin))

	// Downgrade back to member: admin tuple gone, member resolves again.
	require.NoError(t, impl.DowngradeAdmin(t.Context(), memberDID))
	require.False(t, checkOrgRole(t, fga, memberDID, orgID.DID, fgastore.RelationAdmin))
	require.True(t, checkOrgRole(t, fga, memberDID, orgID.DID, fgastore.RelationMember))

	// Remove the member entirely.
	require.NoError(t, impl.RemoveMembers(t.Context(), []syntax.DID{memberDID}))
	require.False(t, checkOrgRole(t, fga, memberDID, orgID.DID, fgastore.RelationMember))
}

func TestBackfillFGA(t *testing.T) {
	fga := newTestFGA(t)
	// Build a store without fga so the members are only in the DB.
	s := newTestStore(t)
	orgID := syntax.DID("did:plc:org1")
	memberDID := syntax.DID("did:plc:member1")
	adminDID := syntax.DID("did:plc:admin1")
	require.NoError(t, s.db.Create(&member{
		OrgID: orgID, Did: memberDID, Role: MemberRole, LoginID: memberDID.String(),
	}).Error)
	require.NoError(t, s.db.Create(&member{
		OrgID: orgID, Did: adminDID, Role: AdminRole, LoginID: adminDID.String(),
	}).Error)

	require.NoError(t, backfillFGA(t.Context(), s.db, fga))

	require.True(t, checkOrgRole(t, fga, memberDID, orgID, fgastore.RelationMember))
	require.True(t, checkOrgRole(t, fga, adminDID, orgID, fgastore.RelationAdmin))
	require.True(t, checkOrgRole(t, fga, adminDID, orgID, fgastore.RelationMember))
}
