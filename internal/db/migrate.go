package db

import (
	"context"
	"fmt"
	"io/fs"

	"github.com/pressly/goose/v3"
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
// Each store package's Models function returns that package's models, so a
// caller migrates every store it builds by passing their Models functions:
//
//	db.Migrate(ctx, db, migrations.FS, org.Models(), spaces.Models())
func Migrate(ctx context.Context, db *gorm.DB, migrations fs.FS, modelSets ...[]any) error {
	if err := automigrate(db, modelSets...); err != nil {
		return err
	}
	return migrateGoose(ctx, db, migrations)
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

// migrateGoose applies the pending migrations in the root of migrations, with
// goose. Go migrations registered globally with goose.AddMigrationContext are
// applied too.
func migrateGoose(ctx context.Context, db *gorm.DB, migrations fs.FS) error {
	if migrations == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	dialect := DialectOf(db)
	if dialect == "" {
		return fmt.Errorf("unsupported dialect: %s", db.Name())
	}
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect(string(dialect)); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, sqlDB, "."); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
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
