package testutil

import (
	"testing"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/stretchr/testify/require"
)

type testStore struct {
	spaces.Store
	Notifier *testutil.TestNotifier
}

func NewTestStore(t *testing.T) *testStore {
	t.Helper()
	db := db_testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	notifier := &testutil.TestNotifier{}
	s, err := spaces.NewStore(db, fga, eventStore, notifier)
	require.NoError(t, err)
	return &testStore{Store: s, Notifier: notifier}
}
