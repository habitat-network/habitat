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

// Migrate applies, with goose, the SQL migrations in the root of sqlMigrations
// and the given Go migrations, in version order.
//
// Go migrations registered globally with goose.AddMigrationContext are applied
// too.
func Migrate(
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
		return nil, fmt.Errorf("unsupported dialect: %s", db.Dialector.Name())
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
	switch db.Dialector.Name() {
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
		return "", fmt.Errorf("unsupported dialect: %s", db.Dialector.Name())
	}
}
