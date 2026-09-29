// Package testutil provides helpers for creating throwaway databases that hold
// pear's full schema.
package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/cmd/pear/migrations"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/db"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
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
// imports them. internal/db/testutil itself imports no store, so those tests
// can still use it.
func NewPearDB(t *testing.T) *gorm.DB {
	t.Helper()
	d := db_testutil.NewUnmigratedDB(t)
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
