package testutil

import (
	"testing"

	"github.com/habitat-network/habitat/internal/db"
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
	database, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(database, org.Models, hive.Models, login.Models))
	h, err := hive.NewHive("example.com", "pear.example.com", database)
	require.NoError(t, err)
	passwordProvider, err := login.NewPasswordProvider(
		database,
		"pear.example.com",
		[]byte("test-signing-secret-for-org-00000"),
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	require.NoError(t, err)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	store, err := org.NewStore(
		database,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
		passwordProvider,
		fga,
		org.NewEveryoneOrg("everyone.example.com"),
	)
	require.NoError(t, err)
	return store
}
