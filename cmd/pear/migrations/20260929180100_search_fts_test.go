package migrations

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
)

// migratedIndex opens dsn, applies all of pear's migrations, and returns a
// search index over it that clears the index before each use, so subtests
// share one database.
func migratedIndex(t *testing.T, dsn string) func(*testing.T) search.Index {
	t.Helper()
	gormDB, err := db.New(dsn)
	require.NoError(t, err)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })
	require.NoError(t, Run(context.Background(), PearMigrationContext{DB: gormDB}))

	// Running again must be a no-op.
	require.NoError(t, Run(context.Background(), PearMigrationContext{DB: gormDB}))

	return func(t *testing.T) search.Index {
		t.Helper()
		require.NoError(t, gormDB.Session(&gorm.Session{AllowGlobalUpdate: true}).
			Exec("DELETE FROM search_documents").Error)
		idx, err := search.New(gormDB)
		require.NoError(t, err)
		return idx
	}
}

// The migrations build the search index's full-text structures. SQLite needs a
// build with the sqlite_fts5 tag.
func TestSearchIndexSqlite(t *testing.T) {
	searchtest.Run(t, migratedIndex(t, "sqlite://"+t.TempDir()+"/test.db"))
}

func TestSearchIndexPostgres(t *testing.T) {
	ctx := context.Background()
	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("pear"),
		postgres.WithUsername("pear"),
		postgres.WithPassword("pear"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)
	searchtest.Run(t, migratedIndex(t, connStr))
}
