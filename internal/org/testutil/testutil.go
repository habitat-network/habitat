package testutil

import (
	"testing"

	habitatdb "github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func NewTestStore(t *testing.T) org.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h := hive.NewHive("example.com", "pear.example.com", db)
	passwordProvider := login.NewPasswordProvider(
		db,
		"pear.example.com",
		[]byte("test-signing-secret-for-org-00000"),
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	store := org.NewStore(
		db,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
		passwordProvider,
		fga,
	)
	require.NoError(t, habitatdb.AutoMigrate(db, h, passwordProvider, store))
	return store
}
