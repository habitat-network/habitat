package sap

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

func TestOrgManager_CreateAndGet(t *testing.T) {
	db := testutil.NewDB(t)
	require.NoError(t, autoMigrate(db))

	o := newOrgManager(db)
	_, err := o.AddManagedOrg(t.Context(), "did:plc:testorg", "session1")
	require.NoError(t, err)

	info, err := o.GetManagedOrg(t.Context(), "did:plc:testorg")
	require.NoError(t, err)
	require.Equal(t, syntax.DID("did:plc:testorg"), info.DID)

	_, err = o.GetManagedOrg(t.Context(), "did:plc:nonexistent")
	require.Error(t, err)
}

func TestOrgManager_ListOrgs(t *testing.T) {
	db := testutil.NewDB(t)
	require.NoError(t, autoMigrate(db))

	o := newOrgManager(db)
	orgs, err := o.ListManagedOrgs(t.Context())
	require.NoError(t, err)
	require.Empty(t, orgs)
	_, err = o.AddManagedOrg(t.Context(), "did:plc:org1", "sess1")
	require.NoError(t, err)
	_, err = o.AddManagedOrg(t.Context(), "did:plc:org2", "sess2")
	require.NoError(t, err)

	orgs, err = o.ListManagedOrgs(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 2)
}
