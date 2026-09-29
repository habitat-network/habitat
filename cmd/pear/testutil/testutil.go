// Package testutil provides helpers for creating throwaway databases that hold
// pear's full schema.
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
// test's temp dir (removed automatically when the test finishes), migrated for
// every store pear persists to. Any store a test builds will find its tables
// already created.
//
// It lives here rather than in internal/db/testutil because it imports all 13
// pear stores, and a store's own in-package tests cannot import a package that
// imports them.
func NewPearDB(t *testing.T) *gorm.DB {
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
	require.NoError(t, db.Migrate(t.Context(), d, migrations.FS,
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
