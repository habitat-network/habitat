package migrations

import (
	"context"
	"database/sql"
	"encoding/json"
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
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/opensocial"
	opensocial_testutil "github.com/habitat-network/habitat/internal/opensocial/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestGrantMcpConfigureSqlite(t *testing.T) {
	requireGrantMcpConfigure(t, db_testutil.NewDB(t, grantMcpConfigureModels()...))
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
	require.NoError(t, db.Migrate(ctx, gormDB, nil, grantMcpConfigureModels()...))
	requireGrantMcpConfigure(t, gormDB)
}

// TestGrantMcpConfigureNoOrgsNeedsNoStore covers a fresh database, which has no
// orgs and so runs the migration without an opensocial store in the context.
func TestGrantMcpConfigureNoOrgsNeedsNoStore(t *testing.T) {
	gormDB := db_testutil.NewDB(t, grantMcpConfigureModels()...)
	requireGrantMcpConfigureRun(t, gormDB, nil, upGrantMcpConfigure)
	requireGrantMcpConfigureRun(t, gormDB, nil, downGrantMcpConfigure)
}

// grantMcpConfigureModels returns the models of the stores the migration reads.
func grantMcpConfigureModels() [][]any {
	return [][]any{opensocial.Models(), spaces.Models(), hive.Models()}
}

// requireGrantMcpConfigureRun runs fn in a transaction on gormDB, with a pear
// migration context holding store, the way [Run] would.
func requireGrantMcpConfigureRun(
	t *testing.T,
	gormDB *gorm.DB,
	store *opensocial.Store,
	fn func(context.Context, *sql.Tx) error,
) {
	t.Helper()
	ctx := context.WithValue(
		t.Context(),
		pearMigrationContextKey{},
		PearMigrationContext{DB: gormDB, Opensocial: store},
	)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, fn(ctx, tx))
	require.NoError(t, tx.Commit())
}

// requireGrantMcpConfigure seeds an org predating mcp.configure, one that
// already bound it to a custom role, and one with no permissions record, then
// asserts that up binds mcp.configure to the admin role in the first and
// leaves the others as is.
func requireGrantMcpConfigure(t *testing.T, gormDB *gorm.DB) {
	t.Helper()
	testStore := opensocial_testutil.NewTestStore(t, opensocial_testutil.WithDB(gormDB))
	store := testStore.SpaceStore

	invite := opensocial_api.CommunityOpensocialPermissionsActionBinding{
		Action: string(opensocial.ActionInvite), Roles: []string{opensocial.AdminRoleRkey},
	}
	custom := opensocial_api.CommunityOpensocialPermissionsActionBinding{
		Action: string(opensocial.ActionMcpConfigure), Roles: []string{"custom"},
	}
	legacyOrg := syntax.DID("did:web:legacy.example.com")
	boundOrg := syntax.DID("did:web:bound.example.com")
	bareOrg := syntax.DID("did:web:bare.example.com")
	putPermissions(t, store, legacyOrg, invite)
	putPermissions(t, store, boundOrg, invite, custom)
	_, err := store.CreateSpace(t.Context(), bareOrg, opensocial.MembersSpaceType, "self")
	require.NoError(t, err)
	boundRev := getPermissions(t, store, boundOrg).Rev

	requireGrantMcpConfigureRun(t, gormDB, testStore.Store, upGrantMcpConfigure)

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
	_, err = store.GetRecord(t.Context(),
		habitat_syntax.ConstructSpaceURI(bareOrg, opensocial.MembersSpaceType, "self"),
		bareOrg, opensocial.PermissionsCollection, "self")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound, "org without a record must not get one")

	// Running it again changes nothing.
	legacyRev := getPermissions(t, store, legacyOrg).Rev
	requireGrantMcpConfigureRun(t, gormDB, testStore.Store, upGrantMcpConfigure)
	require.Equal(t, legacyRev, getPermissions(t, store, legacyOrg).Rev)

	requireGrantMcpConfigureRun(t, gormDB, testStore.Store, downGrantMcpConfigure)
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
	raw, err := json.Marshal(record.Value)
	require.NoError(t, err)
	var permissions opensocial_api.CommunityOpensocialPermissions
	require.NoError(t, json.Unmarshal(raw, &permissions))
	bindings := map[string][]string{}
	for _, binding := range permissions.Bindings {
		bindings[binding.Action] = binding.Roles
	}
	return bindings
}
