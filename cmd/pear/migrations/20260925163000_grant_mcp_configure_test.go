package migrations

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/db"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestGrantMcpConfigureSqlite(t *testing.T) {
	requireGrantMcpConfigure(t, db_testutil.NewDB(t))
}

func TestGrantMcpConfigurePostgres(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("pear"),
		postgres.WithUsername("pear"),
		postgres.WithPassword("pear"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	gormDB, err := db.New(connStr)
	require.NoError(t, err)
	requireGrantMcpConfigure(t, gormDB)
}

// requireGrantMcpConfigure seeds an org predating mcp.configure and one that
// already bound it to a custom role, then asserts that up binds mcp.configure
// to the admin role in the first and leaves the second as is.
func requireGrantMcpConfigure(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	store := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(gormDB))
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)

	invite := opensocial_api.CommunityOpensocialPermissionsActionBinding{
		Action: string(opensocial.ActionInvite), Roles: []string{opensocial.AdminRoleRkey},
	}
	custom := opensocial_api.CommunityOpensocialPermissionsActionBinding{
		Action: string(opensocial.ActionMcpConfigure), Roles: []string{"custom"},
	}
	legacyOrg := syntax.DID("did:web:legacy.example.com")
	boundOrg := syntax.DID("did:web:bound.example.com")
	putPermissions(t, store, legacyOrg, invite)
	putPermissions(t, store, boundOrg, invite, custom)
	boundRev := getPermissions(t, store, boundOrg).Rev

	requireInTx(t, sqlDB, upGrantMcpConfigure)

	require.Equal(t,
		map[string][]string{
			string(opensocial.ActionInvite):       {opensocial.AdminRoleRkey},
			string(opensocial.ActionMcpConfigure): {opensocial.AdminRoleRkey},
		},
		decodeBindings(t, getPermissions(t, store, legacyOrg)),
	)
	bound := getPermissions(t, store, boundOrg)
	require.Equal(t, boundRev, bound.Rev, "already-bound org must not be rewritten")
	require.Equal(t,
		map[string][]string{
			string(opensocial.ActionInvite):       {opensocial.AdminRoleRkey},
			string(opensocial.ActionMcpConfigure): {"custom"},
		},
		decodeBindings(t, bound),
	)

	// Running it again changes nothing.
	legacyRev := getPermissions(t, store, legacyOrg).Rev
	requireInTx(t, sqlDB, upGrantMcpConfigure)
	require.Equal(t, legacyRev, getPermissions(t, store, legacyOrg).Rev)

	requireInTx(t, sqlDB, downGrantMcpConfigure)
}

// TestGrantMcpConfigureSkipsMissingTables covers a fresh database, where the
// GORM-managed tables don't exist yet because AutoMigrate runs after migrations.
func TestGrantMcpConfigureSkipsMissingTables(t *testing.T) {
	sqlDB := newSQLite(t)
	requireInTx(t, sqlDB, upGrantMcpConfigure)
	requireInTx(t, sqlDB, downGrantMcpConfigure)
}

func putPermissions(
	t *testing.T,
	store spaces.Store,
	orgDID syntax.DID,
	bindings ...opensocial_api.CommunityOpensocialPermissionsActionBinding,
) {
	t.Helper()
	ctx := context.Background()
	membersSpace, err := store.CreateSpace(ctx, orgDID, opensocial.MembersSpaceType, "self")
	require.NoError(t, err)
	_, _, err = store.PutRecord(ctx, membersSpace, orgDID, opensocial.PermissionsCollection, "self",
		spaces_testutil.MustMarshalRecord(t, opensocial_api.CommunityOpensocialPermissions{
			Bindings: bindings,
			Assignable: []opensocial_api.CommunityOpensocialPermissionsAssignableBinding{
				{Role: opensocial.AdminRoleRkey, Roles: []string{opensocial.AdminRoleRkey}},
			},
		}))
	require.NoError(t, err)
}

func getPermissions(t *testing.T, store spaces.Store, orgDID syntax.DID) *spaces.Record {
	t.Helper()
	record, err := store.GetRecord(context.Background(),
		habitat_syntax.ConstructSpaceURI(orgDID, opensocial.MembersSpaceType, "self"),
		orgDID, opensocial.PermissionsCollection, "self")
	require.NoError(t, err)
	return record
}

// decodeBindings returns the roles bound to each action in a permissions
// record.
func decodeBindings(t *testing.T, record *spaces.Record) map[string][]string {
	t.Helper()
	permissions, err := decodePermissions(record.Value)
	require.NoError(t, err)
	bindings := map[string][]string{}
	for _, binding := range permissions.Bindings {
		bindings[binding.Action] = binding.Roles
	}
	return bindings
}
