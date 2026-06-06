package testutil

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewTestStore(t *testing.T) org.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)
	store, err := org.NewStore(db, h, identity.DefaultDirectory(), "pear.example.com")
	require.NoError(t, err)
	return store
}
