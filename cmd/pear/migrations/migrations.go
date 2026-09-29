// Package migrations applies pear's database migrations: the schema migrations
// Atlas generates into this package's postgres and sqlite directories, and the
// Go data migrations in this package. See README.md.
package migrations

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
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

// Deps are the pear components Go migrations can use. pear builds them before
// running migrations, so migrations don't construct their own.
//
// Components write outside the migration's transaction unless the migration
// scopes them to it with their WithTx method and [db.WrapTx].
type Deps struct {
	// DB is pear's database.
	DB *gorm.DB
	// Spaces is nil where no spaces store has been built, such as in tests
	// that only need the schema, so migrations that use it must not run there.
	Spaces spaces.Store
}

// goMigrations returns the Go migrations that use deps. Add a migration here
// with goose.NewGoMigration; its version must not collide with any other
// migration's. Go migrations that don't need deps can instead register
// themselves with goose.AddMigrationContext, as goose create generates.
func goMigrations(deps Deps) []*goose.Migration {
	return nil
}

// Run applies all of pear's pending migrations to deps.DB, in version order.
func Run(ctx context.Context, deps Deps) error {
	sqlMigrations, err := dialectMigrations(db.DialectOf(deps.DB))
	if err != nil {
		return fmt.Errorf("load schema migrations: %w", err)
	}
	return db.Up(ctx, deps.DB, sqlMigrations, goMigrations(deps)...)
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
