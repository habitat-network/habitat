package org

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/pkg/org"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestManager(t *testing.T) (*manager, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	m, err := NewManager(org.Org{Domain: "test.example"}, db)
	require.NoError(t, err)
	return m.(*manager), db
}

func seedAdmin(t *testing.T, db *gorm.DB, did syntax.DID) {
	t.Helper()
	require.NoError(t, db.Create(&member{
		Member:    did.String(),
		Role:      string(org.Admin),
		CreatedAt: time.Now(),
	}).Error)
}

var (
	adminDID  = syntax.DID("did:plc:admin111")
	admin2DID = syntax.DID("did:plc:admin222")
	memberDID = syntax.DID("did:plc:member1")
)

func TestGetAdmins(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	admins, err := m.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, adminDID, admins[0])
}

func TestGetMembers(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	// Initially only the admin
	members, err := m.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 1)

	// Add a member
	require.NoError(t, m.AddMembers(t.Context(), adminDID, []syntax.DID{memberDID}))

	members, err = m.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 2)
}

func TestAddAdmin_NonAdminRejected(t *testing.T) {
	m, _ := newTestManager(t)

	err := m.AddAdmin(t.Context(), memberDID, admin2DID)
	require.ErrorIs(t, err, ErrNotAdmin)
}

func TestAddAdmin(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	require.NoError(t, m.AddAdmin(t.Context(), adminDID, admin2DID))

	admins, err := m.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 2)
}

func TestAddMembers_NonAdminRejected(t *testing.T) {
	m, _ := newTestManager(t)

	err := m.AddMembers(t.Context(), memberDID, []syntax.DID{admin2DID})
	require.ErrorIs(t, err, ErrNotAdmin)
}

func TestAddMembers(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	require.NoError(t, m.AddMembers(t.Context(), adminDID, []syntax.DID{memberDID, admin2DID}))

	members, err := m.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 3) // admin + 2 new members
}

func TestRemoveAdmin_LastAdminBlocked(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	err := m.RemoveAdmin(t.Context(), adminDID, adminDID)
	require.ErrorIs(t, err, ErrLastAdmin)
}

func TestRemoveAdmin(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)
	seedAdmin(t, db, admin2DID)

	require.NoError(t, m.RemoveAdmin(t.Context(), adminDID, admin2DID))

	admins, err := m.GetAdmins(t.Context())
	require.NoError(t, err)
	require.Len(t, admins, 1)
	require.Equal(t, adminDID, admins[0])
}

func TestRemoveAdmin_NonAdminRejected(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	err := m.RemoveAdmin(t.Context(), memberDID, adminDID)
	require.ErrorIs(t, err, ErrNotAdmin)
}

func TestRemoveMembers(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)
	require.NoError(t, m.AddMembers(t.Context(), adminDID, []syntax.DID{memberDID}))

	require.NoError(t, m.RemoveMembers(t.Context(), adminDID, []syntax.DID{memberDID}))

	members, err := m.GetMembers(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 1) // only the admin remains
}

func TestRemoveMembers_NonAdminRejected(t *testing.T) {
	m, _ := newTestManager(t)

	err := m.RemoveMembers(t.Context(), memberDID, []syntax.DID{admin2DID})
	require.ErrorIs(t, err, ErrNotAdmin)
}

func TestIsMember(t *testing.T) {
	m, db := newTestManager(t)
	seedAdmin(t, db, adminDID)

	ok, err := m.isMember(t.Context(), adminDID)
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = m.isMember(t.Context(), memberDID)
	require.NoError(t, err)
	require.False(t, ok)
}
