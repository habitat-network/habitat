package testutil

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
)

func NewTestStore(t *testing.T) org.Store {
	t.Helper()
	database := testutil.NewPearDB(t)
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
