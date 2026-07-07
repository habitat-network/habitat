package events

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"
)

func TestStore_Concurrency(t *testing.T) {
	store, err := NewStore(testutil.NewDB(t))
	require.NoError(t, err)

	go func() { require.ErrorIs(t, store.StartSequencer(t.Context()), context.Canceled) }()

	const numWriters = 10
	const eventsPerWriter = 10
	const totalEvents = numWriters * eventsPerWriter

	clock := syntax.NewTIDClock(0)
	for i := range numWriters {
		go func(id int) {
			prev := clock.Next()
			for range eventsPerWriter {
				tid := clock.Next()
				repo := syntax.DID(fmt.Sprintf("did:plc:repo%d", id))
				err := store.AppendSpaceEvent(
					t.Context(),
					"space",
					repo,
					tid,
					prev,
					nil,
				)
				require.NoError(t, err)
				prev = tid
				t.Logf("appended %s to %s", tid, repo)
				store.NotifyEvent(t.Context())
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

func TestStore_SubscriberDoesntBlock(t *testing.T) {
	store, err := NewStore(testutil.NewDB(t))
	require.NoError(t, err)
	go func() { require.ErrorIs(t, store.StartSequencer(t.Context()), context.Canceled) }()

	subscriberChan1 := store.Subscribe(t.Context(), 0)
	subscriberChan2 := store.Subscribe(t.Context(), 0)

	var wg sync.WaitGroup

	wg.Go(func() {
		// only drain subscriber 1 for 10 events
		for e := range subscriberChan1 {
			if e.Seq >= 10 {
				return
			}
		}
	})

	clock := syntax.NewTIDClock(0)
	wg.Go(func() {
		prev := clock.Next()
		for range 10 {
			tid := clock.Next()
			err := store.AppendSpaceEvent(
				t.Context(),
				"space",
				"did:plc:test",
				tid,
				prev,
				nil,
			)
			require.NoError(t, err)
			prev = tid
			store.NotifyEvent(t.Context())

		}
	})

	wg.Wait()

	prev := clock.Next()
	for range 10 {
		tid := clock.Next()
		err := store.AppendSpaceEvent(
			t.Context(),
			"space",
			"did:plc:test",
			tid,
			prev,
			nil,
		)
		require.NoError(t, err)
		prev = tid
		store.NotifyEvent(t.Context())
	}

	for e := range subscriberChan2 {
		if e.Seq >= 20 {
			break
		}
	}
}
