// Package testutil provides helpers for creating throwaway databases for
// pkg/sap's tests.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap/crawl"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
	"github.com/habitat-network/habitat/pkg/sap/register"
	"github.com/habitat-network/habitat/pkg/sap/session"
	"github.com/habitat-network/habitat/pkg/sap/syncer"
)

// NewSapDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes), migrated for
// every store sap persists to. It is the sap counterpart to
// db/testutil.NewPearDB.
//
// sap gets its own helper rather than sharing pear's because the two schemas
// collide: internal/notify and pkg/sap/register both claim a `registrations`
// table, with different primary keys. Put them on one database and whichever
// migrates second rewrites the other's table, so a test must pick one.
//
// The file is opened in WAL journal mode with a busy timeout, so the tests can
// use gorm's connection pool for concurrent reads and writes without hitting
// "database is locked" errors — unlike a ":memory:" database, where each pooled
// connection would see a separate, empty database.
//
// gorm logs are routed through t.Logf so they only appear when the test runs
// verbosely or fails.
func NewSapDB(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	d, err := db.New("sqlite://" + path)
	require.NoError(t, err)
	d.Logger = logger.New(testLog{t: t}, logger.Config{
		LogLevel:                  logger.Info,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      false,
		Colorful:                  true,
	})
	// sap has no goose migrations, so Migrate only creates the tables.
	require.NoError(t, db.Migrate(t.Context(), d, nil,
		crawl.Models(),
		outbox.Models(),
		register.Models(),
		session.Models(),
		syncer.Models(),
		oauthclient.Models(),
	))
	return d
}

// testLog routes gorm's log output through t.Logf.
type testLog struct {
	t *testing.T
}

func (w testLog) Printf(format string, args ...any) {
	w.t.Logf(format, args...)
}
