package sap

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOrgManager_CreateAndGet(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))

	o := newOrgManager(db, "sap.domain", "secret")
	_, err = o.AddManagedOrg(t.Context(), "did:plc:testorg", "session1")
	require.NoError(t, err)

	info, err := o.GetManagedOrg(t.Context(), "did:plc:testorg")
	require.NoError(t, err)
	require.Equal(t, "did:plc:testorg", info.DID)

	_, err = o.GetManagedOrg(t.Context(), "did:plc:nonexistent")
	require.Error(t, err)
}

func TestOrgManager_GetOrgWithoutSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))

	require.NoError(t, db.Create(&managedOrg{DID: "did:plc:testorg"}).Error)

	o := newOrgManager(db, "", "")
	_, err = o.GetManagedOrg(t.Context(), "did:plc:testorg")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no session")
}

func TestOrgManager_ListOrgs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))

	o := newOrgManager(db, "", "")
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
