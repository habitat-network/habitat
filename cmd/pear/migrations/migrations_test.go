package migrations

import (
	"context"
	"database/sql"
	"errors"
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
)

func TestDialectMigrations(t *testing.T) {
	for _, dialect := range []db.Dialect{db.Postgres, db.Sqlite} {
		migrations, err := dialectMigrations(dialect)
		require.NoError(t, err)
		files, err := fs.Glob(migrations, "*.sql")
		require.NoError(t, err)
		require.NotEmpty(t, files, dialect)
	}
	_, err := dialectMigrations("mysql")
	require.Error(t, err)
}

// A Go migration gets the PearMigrationContext passed to db.Up's context,
// scoped to its transaction.
func TestGetPearMigrationContextScopesToMigrationTx(t *testing.T) {
	d := db_testutil.NewDB(t)
	require.NoError(t, d.Exec("CREATE TABLE seen (n INTEGER)").Error)
	ctx := context.WithValue(t.Context(), pearMigrationContextKey{}, PearMigrationContext{DB: d})

	errRollback := errors.New("roll back")
	migration := goose.NewGoMigration(
		20990101000000,
		&goose.GoFunc{RunTx: func(ctx context.Context, tx *sql.Tx) error {
			mc, err := GetPearMigrationContext(ctx, tx)
			require.NoError(t, err)
			require.Nil(t, mc.Spaces)
			require.NoError(t, mc.DB.Exec("INSERT INTO seen (n) VALUES (1)").Error)
			return errRollback
		}},
		nil,
	)
	require.ErrorIs(t, db.Up(ctx, d, nil, migration), errRollback)

	// The insert ran on the migration's transaction, so it rolled back with it.
	var count int64
	require.NoError(t, d.Raw("SELECT count(*) FROM seen").Scan(&count).Error)
	require.Zero(t, count)
}

func TestGetPearMigrationContextOutsideRun(t *testing.T) {
	_, err := GetPearMigrationContext(t.Context(), nil)
	require.Error(t, err)
}
