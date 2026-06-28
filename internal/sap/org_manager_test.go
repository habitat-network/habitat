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

	o := NewOrgManager(db, "sap.domain", "secret")
	require.NoError(t, o.CreateOrg(t.Context(), "did:plc:testorg", "session1"))

	info, err := o.GetOrg(t.Context(), "did:plc:testorg")
	require.NoError(t, err)
	require.Equal(t, "did:plc:testorg", info.DID)

	_, err = o.GetOrg(t.Context(), "did:plc:nonexistent")
	require.Error(t, err)
}

func TestOrgManager_GetOrgWithoutSession(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))

	require.NoError(t, db.Create(&managedOrg{DID: "did:plc:testorg"}).Error)

	o := NewOrgManager(db, "", "")
	_, err = o.GetOrg(t.Context(), "did:plc:testorg")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no session")
}

func TestOrgManager_ListOrgs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, autoMigrate(db))

	o := NewOrgManager(db, "", "")
	orgs, err := o.ListOrgs(t.Context())
	require.NoError(t, err)
	require.Empty(t, orgs)

	require.NoError(t, o.CreateOrg(t.Context(), "did:plc:org1", "sess1"))
	require.NoError(t, o.CreateOrg(t.Context(), "did:plc:org2", "sess2"))

	orgs, err = o.ListOrgs(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 2)

	// Orgs without a SessionID are not listed
	require.NoError(t, db.Create(&managedOrg{DID: "did:plc:no-session"}).Error)
	orgs, err = o.ListOrgs(t.Context())
	require.NoError(t, err)
	require.Len(t, orgs, 2)
}
