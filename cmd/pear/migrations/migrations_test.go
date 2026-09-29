package migrations

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db"
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
