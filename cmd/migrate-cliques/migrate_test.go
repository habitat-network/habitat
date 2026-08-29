package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestMigrateCliques(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)

	fga, err := fgastore.NewSQLite(ctx, dbPath)
	require.NoError(t, err)
	defer fga.Close()

	cliqueStore, err := clique.NewStore(db)
	require.NoError(t, err)

	_, err = permissions.NewStore(db, cliqueStore)
	require.NoError(t, err)

	spacesStore, err := spaces.NewStore(db, fga)
	require.NoError(t, err)

	owner, err := syntax.ParseDID("did:plc:alice")
	require.NoError(t, err)
	bob, err := syntax.ParseDID("did:plc:bob")
	require.NoError(t, err)
	carol, err := syntax.ParseDID("did:plc:carol")
	require.NoError(t, err)

	// Create a clique and add members.
	cliqueURI, err := cliqueStore.CreateClique(ctx, owner, []syntax.DID{bob, carol})
	require.NoError(t, err)

	// Insert a permission row referencing this clique for network.habitat.docs.
	err = db.Exec(
		`INSERT INTO permissions (grantee, owner, collection, rkey) VALUES (?, ?, ?, ?)`,
		cliqueURI.String(), owner.String(), "network.habitat.docs", "doc1",
	).Error
	require.NoError(t, err)

	// Run migration.
	count, err := MigrateCliques(ctx, db, cliqueStore, spacesStore)
	require.NoError(t, err)
	require.Equal(t, 1, count)

	// Verify space was created.
	spacesList, err := spacesStore.ListSpaces(ctx, owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spacesList, 1)
	require.Equal(t, "network.habitat.docs", spacesList[0].Type.String())
	require.Equal(t, habitat_syntax.Skey(cliqueURI.Key()), spacesList[0].Skey)

	// Verify records in space_records have collection network.habitat.docs.edit.
	var recordCollection string
	err = db.Raw(
		`SELECT collection FROM space_records WHERE space = ?`,
		spacesList[0].URI.String(),
	).Scan(&recordCollection).Error
	require.NoError(t, err)
	require.Equal(t, "network.habitat.docs.edit", recordCollection)

	// Verify members were added (owner is always a member, plus bob and carol).
	members, err := spacesStore.GetMembers(ctx, spacesList[0].URI)
	require.NoError(t, err)
	require.Len(t, members, 3)
	memberDIDs := make([]string, len(members))
	for i, m := range members {
		memberDIDs[i] = m.Did.String()
	}
	require.ElementsMatch(t, []string{owner.String(), bob.String(), carol.String()}, memberDIDs)
}

func TestMigrateCliques_Idempotent(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)

	fga, err := fgastore.NewSQLite(ctx, dbPath)
	require.NoError(t, err)
	defer fga.Close()

	cliqueStore, err := clique.NewStore(db)
	require.NoError(t, err)

	_, err = permissions.NewStore(db, cliqueStore)
	require.NoError(t, err)

	spacesStore, err := spaces.NewStore(db, fga)
	require.NoError(t, err)

	owner, _ := syntax.ParseDID("did:plc:alice")
	bob, _ := syntax.ParseDID("did:plc:bob")

	cliqueURI, err := cliqueStore.CreateClique(ctx, owner, []syntax.DID{bob})
	require.NoError(t, err)

	err = db.Exec(
		`INSERT INTO permissions (grantee, owner, collection, rkey) VALUES (?, ?, ?, ?)`,
		cliqueURI.String(), owner.String(), "network.habitat.docs", "doc1",
	).Error
	require.NoError(t, err)

	// First run.
	count1, err := MigrateCliques(ctx, db, cliqueStore, spacesStore)
	require.NoError(t, err)
	require.Equal(t, 1, count1)

	// Second run — should skip existing spaces.
	count2, err := MigrateCliques(ctx, db, cliqueStore, spacesStore)
	require.NoError(t, err)
	require.Equal(t, 0, count2)
}

func TestMigrateCliques_OnlyDocsCollections(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	require.NoError(t, err)

	fga, err := fgastore.NewSQLite(ctx, dbPath)
	require.NoError(t, err)
	defer fga.Close()

	cliqueStore, err := clique.NewStore(db)
	require.NoError(t, err)

	_, err = permissions.NewStore(db, cliqueStore)
	require.NoError(t, err)

	spacesStore, err := spaces.NewStore(db, fga)
	require.NoError(t, err)

	owner, _ := syntax.ParseDID("did:plc:alice")
	bob, _ := syntax.ParseDID("did:plc:bob")

	// Clique + permission for a non-docs collection.
	cliqueURI, err := cliqueStore.CreateClique(ctx, owner, []syntax.DID{bob})
	require.NoError(t, err)
	err = db.Exec(
		`INSERT INTO permissions (grantee, owner, collection, rkey) VALUES (?, ?, ?, ?)`,
		cliqueURI.String(), owner.String(), "network.habitat.other", "rec1",
	).Error
	require.NoError(t, err)

	// Should not migrate anything because the collection doesn't match.
	count, err := MigrateCliques(ctx, db, cliqueStore, spacesStore)
	require.NoError(t, err)
	require.Equal(t, 0, count)
}
