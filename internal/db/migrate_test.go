package db

import (
	"context"
	"database/sql"
	"testing"
	"testing/fstest"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/utils/tests"
)

func newSqlite(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := New("sqlite://" + t.TempDir() + "/test.db")
	require.NoError(t, err)
	return db
}

func TestMigrateRunsSQLAndGoMigrations(t *testing.T) {
	db := newSqlite(t)
	sqlMigrations := fstest.MapFS{
		"20260101000000_create_widgets.sql": &fstest.MapFile{
			Data: []byte(`-- +goose Up
CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT);

-- +goose Down
DROP TABLE widgets;
`),
		},
	}
	// The Go migration runs after the SQL one, so it can write to its table.
	seed := goose.NewGoMigration(20260102000000,
		&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
			_, err := tx.ExecContext(ctx, "INSERT INTO widgets (name) VALUES ('a')")
			return err
		}},
		nil,
	)

	require.NoError(t, Migrate(t.Context(), db, sqlMigrations, seed))

	var count int64
	require.NoError(t, db.Table("widgets").Count(&count).Error)
	require.Equal(t, int64(1), count)

	// Applied migrations are recorded, so running again is a no-op rather than
	// a duplicate insert.
	require.NoError(t, Migrate(t.Context(), db, sqlMigrations, seed))
	require.NoError(t, db.Table("widgets").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestMigrateFailsOnBadMigration(t *testing.T) {
	db := newSqlite(t)
	err := Migrate(t.Context(), db, fstest.MapFS{
		"20260101000000_bad.sql": &fstest.MapFile{
			Data: []byte("-- +goose Up\nNOT SQL;\n"),
		},
	})
	require.Error(t, err)
}

func TestWrapTxRunsOnTx(t *testing.T) {
	db := newSqlite(t)
	require.NoError(t, db.Exec("CREATE TABLE widgets (id INTEGER PRIMARY KEY, name TEXT)").Error)
	sqlDB, err := db.DB()
	require.NoError(t, err)

	tx, err := sqlDB.BeginTx(t.Context(), nil)
	require.NoError(t, err)
	wrapped, err := WrapTx(db, tx)
	require.NoError(t, err)
	require.NoError(t, wrapped.Exec("INSERT INTO widgets (name) VALUES ('a')").Error)
	require.NoError(t, tx.Rollback())

	// The insert was part of the rolled-back transaction.
	var count int64
	require.NoError(t, db.Table("widgets").Count(&count).Error)
	require.Equal(t, int64(0), count)
}

func TestDialectOf(t *testing.T) {
	require.Equal(t, Sqlite, DialectOf(newSqlite(t)))
}

func TestMigrateRejectsDuplicateGoMigrationVersions(t *testing.T) {
	noop := func() *goose.Migration {
		return goose.NewGoMigration(20260101000000, &goose.GoFunc{}, &goose.GoFunc{})
	}
	require.Error(t, Migrate(t.Context(), newSqlite(t), nil, noop(), noop()))
}

func TestUnsupportedDialect(t *testing.T) {
	db, err := gorm.Open(tests.DummyDialector{}, &gorm.Config{})
	require.NoError(t, err)
	require.Empty(t, DialectOf(db))
	require.Error(t, Migrate(t.Context(), db, nil))
	_, err = WrapTx(db, nil)
	require.Error(t, err)
}

func TestDialectOfPostgres(t *testing.T) {
	// pgx connects lazily, so this opens without a server.
	db, err := gorm.Open(
		postgres.Open("postgres://localhost:1/none"),
		&gorm.Config{DisableAutomaticPing: true},
	)
	require.NoError(t, err)
	require.Equal(t, Postgres, DialectOf(db))
	dialect, err := gooseDialect(db)
	require.NoError(t, err)
	require.Equal(t, goose.DialectPostgres, dialect)
}
