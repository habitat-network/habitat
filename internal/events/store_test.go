package events

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(
		sqlite.Open(filepath.Join(t.TempDir(), "test.db")+"?_journal_mode=WAL&_busy_timeout=5000"),
		&gorm.Config{},
	)
	require.NoError(t, err)
	return db
}

func TestStore_Concurrency(t *testing.T) {
	db := openTestDB(t)
	store, err := NewStore(db)
	require.NoError(t, err)

	go store.StartSequencer(t.Context())

	const numWriters = 10
	const eventsPerWriter = 10
	const totalEvents = numWriters * eventsPerWriter

	for i := range numWriters {
		clock := syntax.NewTIDClock(0)
		go func(id int) {
			prev := clock.Next()
			for range eventsPerWriter {
				tid := clock.Next()
				repo := syntax.DID(fmt.Sprintf("did:plc:repo%d", id))
				store.AppendSpaceEvent(
					t.Context(),
					"space",
					repo,
					tid,
					prev,
					nil,
				)
				prev = tid
				t.Logf("appended %s to %s", tid, repo)
				store.NotifyEvent(t.Context())
				// time.Sleep(10 * time.Millisecond)
			}
		}(i)
	}

	var wg sync.WaitGroup

	const numSubs = 10
	for i := range numSubs {
		wg.Go(func() {
			events := store.Subscribe(t.Context(), 0)
			var lastSeen uint64
			for e := range events {
				t.Logf("sub %d received seq %d for %s to %s", i, e.Seq, e.Rev, e.Repo)
				require.Greater(t, e.Rev, e.Since)
				require.Greater(t, e.Seq, lastSeen)
				lastSeen = e.Seq

				if lastSeen >= totalEvents {
					return
				}
			}
		})
	}

	wg.Wait()
}
