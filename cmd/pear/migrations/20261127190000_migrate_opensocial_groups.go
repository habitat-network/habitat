package migrations

import (
	"context"
	"database/sql"

	"github.com/pressly/goose/v3"

	"github.com/habitat-network/habitat/internal/opensocial"
)

func init() {
	goose.AddMigrationContext(upMigrateOpensocialGroups, downMigrateOpensocialGroups)
}

// upMigrateOpensocialGroups rewrites groups stored under the
// community.opensocial names as org.atgrp groups, which the schema and the
// code now expect: see [opensocial.MigrateLegacyGroups]. It runs after the
// schema migrations that create the join request and moderation tables.
func upMigrateOpensocialGroups(ctx context.Context, tx *sql.Tx) error {
	// goose runs globally registered Go migrations for every provider, including
	// fgastore's own, which has no pear context and nothing to migrate.
	if _, ok := ctx.Value(pearMigrationContextKey{}).(PearMigrationContext); !ok {
		return nil
	}
	mc, err := GetPearMigrationContext(ctx, tx)
	if err != nil {
		return err
	}
	// Tests that only need the schema build no spaces store, and have no groups.
	if mc.Spaces == nil {
		return nil
	}
	postgres, err := isPostgres(ctx, tx)
	if err != nil {
		return err
	}
	for _, table := range []string{"spaces", "invite_rows"} {
		exists, err := tableExists(ctx, tx, table, postgres)
		if err != nil || !exists {
			return err
		}
	}
	return opensocial.MigrateLegacyGroups(ctx, mc.DB, mc.Spaces)
}

// downMigrateOpensocialGroups does nothing: the old names are not written back.
func downMigrateOpensocialGroups(context.Context, *sql.Tx) error { return nil }
