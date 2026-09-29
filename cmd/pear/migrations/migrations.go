// Package migrations applies pear's database migrations: the schema migrations
// Atlas generates into this package's postgres and sqlite directories, and the
// Go data migrations in this package. See README.md.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"

	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/spaces"
)

// schemaMigrations holds the SQL migrations Atlas generates from [Models], one
// directory per dialect. Do not edit them by hand without rehashing (moon run
// pear:migration-hash).
//
//go:embed postgres/*.sql sqlite/*.sql
var schemaMigrations embed.FS

// PearMigrationContext holds the pear components Go migrations can use. pear
// builds them before running migrations, so migrations don't construct their
// own. [Run] puts it on the context goose passes to each Go migration, which
// reads it back with [GetPearMigrationContext].
type PearMigrationContext struct {
	// DB is pear's database.
	DB *gorm.DB
	// Spaces is nil where no spaces store has been built, such as in tests
	// that only need the schema, so migrations that use it must not run there.
	Spaces spaces.Store
}

type pearMigrationContextKey struct{}

// GetPearMigrationContext returns the [PearMigrationContext] that [Run] put on
// ctx, scoped to tx, the migration's transaction: DB and every component that
// supports WithTx run their statements on tx, so they commit or roll back with
// the migration.
func GetPearMigrationContext(ctx context.Context, tx *sql.Tx) (PearMigrationContext, error) {
	mc, ok := ctx.Value(pearMigrationContextKey{}).(PearMigrationContext)
	if !ok {
		return PearMigrationContext{}, errors.New(
			"no pear migration context; migrations must run with Run",
		)
	}
	gormTx, err := db.WrapTx(mc.DB, tx)
	if err != nil {
		return PearMigrationContext{}, err
	}
	scoped := PearMigrationContext{DB: gormTx}
	if mc.Spaces != nil {
		scoped.Spaces = mc.Spaces.WithTx(gormTx)
	}
	return scoped, nil
}

// Run applies all of pear's pending migrations to mc.DB, in version order. Go
// migrations register themselves with goose.AddMigrationContext and get mc
// from their context with [GetPearMigrationContext].
func Run(ctx context.Context, mc PearMigrationContext) error {
	sqlMigrations, err := dialectMigrations(db.DialectOf(mc.DB))
	if err != nil {
		return fmt.Errorf("load schema migrations: %w", err)
	}
	return db.Up(context.WithValue(ctx, pearMigrationContextKey{}, mc), mc.DB, sqlMigrations)
}

// dialectMigrations returns the schema migrations for dialect.
func dialectMigrations(dialect db.Dialect) (fs.FS, error) {
	switch dialect {
	case db.Postgres:
		return fs.Sub(schemaMigrations, "postgres")
	case db.Sqlite:
		return fs.Sub(schemaMigrations, "sqlite")
	default:
		return nil, fmt.Errorf("unsupported dialect: %q", dialect)
	}
}
