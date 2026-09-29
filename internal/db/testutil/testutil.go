// Package testutil provides helpers for creating throwaway databases in tests.
package testutil

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/instance"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/notify"
	"github.com/habitat-network/habitat/internal/oauthserver"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	"github.com/habitat-network/habitat/internal/spaces"
)

// NewPearDB returns a gorm DB backed by a temporary SQLite file living in the
// test's temp dir (removed automatically when the test finishes), migrated with
// db.Migrate for every store pear persists to. Any store a test builds will find
// its tables already created.
//
// The file is opened in WAL journal mode with a busy timeout, so the tests can
// use gorm's connection pool for concurrent reads and writes without hitting
// "database is locked" errors — unlike a ":memory:" database, where each pooled
// connection would see a separate, empty database.
//
// gorm logs are routed through t.Logf so they only appear when the test runs
// verbosely or fails.
func NewPearDB(t *testing.T) *gorm.DB {
	t.Helper()
	d := NewUnmigratedDB(t)
	require.NoError(t, db.Migrate(t.Context(), d, migrations.FS, pearModels()...))
	return d
}

// pearModels returns the models of every store pear persists to, so tests that
// only exercise a few of them don't have to name which tables they need.
func pearModels() [][]any {
	return [][]any{
		clique.Models(),
		emaildomain.Models(),
		hive.Models(),
		instance.Models(),
		login.Models(),
		notify.Models(),
		oauthserver.Models(),
		opensocial.Models(),
		org.Models(),
		pdscred.Models(),
		permissions.Models(),
		repo.Models(),
		spaces.Models(),
	}
}

// NewUnmigratedDB is like [NewPearDB] but creates no tables, for tests whose
// stores migrate themselves — pkg/sap, cmd/home and cmd/search share a database
// with pear's own stores and create their own tables.
func NewUnmigratedDB(t *testing.T) *gorm.DB {
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
	return d
}

// testLog routes gorm's log output through t.Logf.
type testLog struct {
	t *testing.T
}

func (w testLog) Printf(format string, args ...any) {
	w.t.Logf(format, args...)
}
