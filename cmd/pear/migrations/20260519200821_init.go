package migrations

import (
	"context"
	"database/sql"
	"github.com/pressly/goose/v3"
)

func init() {
	goose.AddMigrationContext(upInit, downInit)
}

func upInit(ctx context.Context, tx *sql.Tx) error {
	// This code is executed when the migration is applied.
	return nil
}

func downInit(ctx context.Context, tx *sql.Tx) error {
	// This code is executed when the migration is rolled back.
	return nil
}
