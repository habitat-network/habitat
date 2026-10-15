// Package keyvaltest starts Postgres for keyval tests, since the SQLite
// harness in internal/db/testutil cannot exercise the Postgres dialect.
package keyvaltest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/db"
)

// PGURLEnv names an environment variable holding the URL of a running
// Postgres for tests to use instead of starting a container.
const PGURLEnv = "HABITAT_TEST_PGURL"

var (
	startOnce sync.Once
	sharedURL string
	startErr  error
)

// NewDB returns a handle to a fresh database on the shared Postgres server,
// from [PGURLEnv] or a container started on first use. The database has no
// tables: it cannot migrate keyval's models without importing keyval, which
// would be an import cycle with keyval's own tests, so callers migrate it
// themselves. Each call gets its own database, so tests that count rows do not see
// each other's.
func NewDB(t *testing.T) *gorm.DB {
	t.Helper()
	serverURL := pgURL(t)

	admin, err := db.New(serverURL)
	require.NoError(t, err)

	suffix := make([]byte, 8)
	_, err = rand.Read(suffix)
	require.NoError(t, err)
	name := "keyval_" + hex.EncodeToString(suffix)
	require.NoError(t, admin.Exec(`CREATE DATABASE "`+name+`"`).Error)

	u, err := url.Parse(serverURL)
	require.NoError(t, err)
	u.Path = "/" + name
	d, err := db.New(u.String())
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := d.DB(); err == nil {
			_ = sqlDB.Close()
		}
		// The test's own context is already cancelled by now. FORCE drops the
		// database even if a connection lingers.
		_ = admin.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`).Error
		if sqlDB, err := admin.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	return d
}

func pgURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv(PGURLEnv); u != "" {
		return u
	}
	startOnce.Do(func() {
		// The container outlives the test that started it, since later tests
		// share it; testcontainers' reaper removes it after the run.
		ctx := context.Background()
		c, err := postgres.Run(ctx,
			"postgres:16-alpine",
			postgres.WithDatabase("keyval"),
			postgres.WithUsername("keyval"),
			postgres.WithPassword("keyval"),
			tc.WithWaitStrategy(
				wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
			),
		)
		if err != nil {
			startErr = err
			return
		}
		sharedURL, startErr = c.ConnectionString(ctx, "sslmode=disable")
	})
	require.NoError(t, startErr)
	return sharedURL
}
