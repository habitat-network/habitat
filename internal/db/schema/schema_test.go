package schema

import (
	"io/fs"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db"
)

func TestMigrations(t *testing.T) {
	for _, dialect := range []db.Dialect{db.Postgres, db.Sqlite} {
		migrations, err := Migrations(dialect)
		require.NoError(t, err)
		files, err := fs.Glob(migrations, "*.sql")
		require.NoError(t, err)
		require.NotEmpty(t, files, dialect)
	}
	_, err := Migrations("mysql")
	require.Error(t, err)
}
