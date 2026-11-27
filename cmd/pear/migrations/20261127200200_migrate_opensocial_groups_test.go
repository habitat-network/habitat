package migrations

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	notify_testutil "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestMigrateOpensocialGroupsRewritesLegacyGroup(t *testing.T) {
	ctx := t.Context()
	d := db_testutil.NewDB(t)
	require.NoError(t, Run(ctx, PearMigrationContext{DB: d}))
	store, err := spaces.NewStore(d, &notify_testutil.TestNotifier{})
	require.NoError(t, err)

	org := syntax.DID("did:plc:acme")
	members := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.members", "self")
	about := habitat_syntax.ConstructSpaceURI(org, "community.opensocial.about", "self")
	for _, space := range []habitat_syntax.SpaceURI{members, about} {
		_, err := store.CreateSpace(ctx, org, syntax.NSID(space.SpaceType()), "self")
		require.NoError(t, err)
	}
	for _, record := range []struct {
		space      habitat_syntax.SpaceURI
		collection string
		rkey       string
		value      map[string]any
	}{
		{about, "community.opensocial.profile", "self", map[string]any{"name": "Acme"}},
		{members, "community.opensocial.role", "admin", map[string]any{"name": "Admin"}},
		{members, "community.opensocial.membership", "did:plc:alice",
			map[string]any{"roles": []any{"admin"}}},
	} {
		record.value["$type"] = record.collection
		raw, err := spaces.MarshalRecord(record.value)
		require.NoError(t, err)
		_, _, err = store.PutRecord(
			ctx, record.space, org, syntax.NSID(record.collection),
			syntax.RecordKey(record.rkey), raw,
		)
		require.NoError(t, err)
	}

	sqlDB, err := d.DB()
	require.NoError(t, err)
	tx, err := sqlDB.BeginTx(ctx, nil)
	require.NoError(t, err)
	migrationCtx := context.WithValue(
		ctx, pearMigrationContextKey{}, PearMigrationContext{DB: d, Spaces: store},
	)
	require.NoError(t, upMigrateOpensocialGroups(migrationCtx, tx))
	require.NoError(t, tx.Commit())

	newMembers := habitat_syntax.ConstructSpaceURI(org, "org.atgrp.members", "self")
	roles, err := store.ListCollection(ctx, newMembers, "org.atgrp.role")
	require.NoError(t, err)
	require.Len(t, roles, 1)
	require.Equal(t, "Admin", roles[0].Value["displayName"])
	memberships, err := store.ListCollection(ctx, newMembers, "org.atgrp.membership")
	require.NoError(t, err)
	require.Len(t, memberships, 1)
	exists, err := store.CheckSpaceExists(ctx, members)
	require.NoError(t, err)
	require.False(t, exists)
}
