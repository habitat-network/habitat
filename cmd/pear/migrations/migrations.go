// Package migrations applies pear's database migrations: the schema migrations
// Atlas generates into internal/db/schema, and the Go data migrations in this
// package. See README.md.
package migrations

import (
	"context"

	"github.com/pressly/goose/v3"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/db/schema"
	"github.com/habitat-network/habitat/internal/spaces"
)

// Deps are the pear components Go migrations can use. pear builds them before
// running migrations, so migrations don't construct their own.
//
// Components write outside the migration's transaction unless the migration
// scopes them to it with their WithTx method and [db.WrapTx].
type Deps struct {
	// DB is pear's database.
	DB     *gorm.DB
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
	sqlMigrations, err := schema.Migrations(db.DialectOf(deps.DB))
	if err != nil {
		return err
	}
	return db.Migrate(ctx, deps.DB, sqlMigrations, goMigrations(deps)...)
}
