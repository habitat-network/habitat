package testutil

import (
	"testing"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type Config struct {
	FgaStore fgastore.Store
	DB       *gorm.DB
}

type testStore struct {
	spaces.Store
	Notifier   *testutil.TestNotifier
	EventStore events.Store
}

func NewTestStore(t *testing.T, cfgs ...Config) *testStore {
	t.Helper()
	cfg := Config{}
	if len(cfgs) > 0 {
		cfg = cfgs[0]
	}
	if cfg.FgaStore == nil {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		t.Cleanup(func() { _ = fga.Close() })
		cfg.FgaStore = fga
	}
	if cfg.DB == nil {
		cfg.DB = db_testutil.NewDB(t)
	}
	eventStore, err := events.NewStore(cfg.DB)
	require.NoError(t, err)
	notifier := &testutil.TestNotifier{}
	s, err := spaces.NewStore(cfg.DB, cfg.FgaStore, eventStore, notifier)
	require.NoError(t, err)
	return &testStore{Store: s, Notifier: notifier, EventStore: eventStore}
}
