package db

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Migrate brings a database up to date: it first creates the tables for the
// given models with GORM's schema inference, then applies the goose migrations
// in migrations.
//
// Migrate is additive. GORM leaves existing tables in place and only adds
// missing columns, indexes and constraints, and goose tracks applied versions
// in its own table, so both steps are no-ops on an already-migrated database.
//
// It suits tests that build a few stores and want just their tables. pear
// itself owns its schema with versioned migrations instead and calls [Up].
//
// Each store package's Models function returns that package's models, so a
// caller migrates every store it builds by passing their Models functions:
//
//	db.Migrate(ctx, db, nil, org.Models(), spaces.Models())
func Migrate(ctx context.Context, db *gorm.DB, migrations fs.FS, modelSets ...[]any) error {
	if err := automigrate(db, modelSets...); err != nil {
		return err
	}
	if migrations == nil {
		return nil
	}
	return Up(ctx, db, migrations)
}

// automigrate creates the tables for every model in modelSets, in one GORM call.
func automigrate(db *gorm.DB, modelSets ...[]any) error {
	models := make([]any, 0, len(modelSets))
	for _, set := range modelSets {
		models = append(models, set...)
	}
	if len(models) == 0 {
		return nil
	}
	if err := db.AutoMigrate(models...); err != nil {
		return fmt.Errorf("automigrate: %w", err)
	}
	return nil
}

// Up applies, with goose, the pending SQL migrations in the root of
// sqlMigrations and the given Go migrations, in version order.
//
// Go migrations registered globally with goose.AddMigrationContext are applied
// too. goMigrations exist for migrations that need components their caller has
// already built, which a globally registered function has no way to reach; see
// [WrapTx] for running such a component inside the migration's transaction.
func Up(
	ctx context.Context,
	db *gorm.DB,
	sqlMigrations fs.FS,
	goMigrations ...*goose.Migration,
) error {
	dialect, err := gooseDialect(db)
	if err != nil {
		return err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(
		dialect,
		sqlDB,
		sqlMigrations,
		goose.WithGoMigrations(goMigrations...),
	)
	if err != nil {
		return fmt.Errorf("create migration provider: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

// WrapTx returns a gorm DB, of the same dialect and config as db, whose
// statements run on tx. Go migrations use it to run stores' WithTx methods
// inside the migration's transaction; GORM nests its own transactions on it
// as savepoints.
func WrapTx(db *gorm.DB, tx *sql.Tx) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch DialectOf(db) {
	case Postgres:
		dialector = postgres.New(postgres.Config{Conn: tx})
	case Sqlite:
		dialector = sqlite.New(sqlite.Config{Conn: tx})
	default:
		return nil, fmt.Errorf("unsupported dialect: %s", db.Name())
	}
	wrapped, err := gorm.Open(dialector, &gorm.Config{
		TranslateError: db.TranslateError,
		Logger:         db.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("open gorm on tx: %w", err)
	}
	return wrapped, nil
}

// DialectOf returns the dialect of an open database, or "" if it is neither
// Postgres nor SQLite.
func DialectOf(db *gorm.DB) Dialect {
	switch db.Name() {
	case "postgres":
		return Postgres
	case "sqlite":
		return Sqlite
	default:
		return ""
	}
}

func gooseDialect(db *gorm.DB) (goose.Dialect, error) {
	switch DialectOf(db) {
	case Postgres:
		return goose.DialectPostgres, nil
	case Sqlite:
		return goose.DialectSQLite3, nil
	default:
		return "", fmt.Errorf("unsupported dialect: %s", db.Name())
	}
}
