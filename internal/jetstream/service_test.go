package jetstream

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/stretchr/testify/require"
)

func TestUpdateServiceUnsubscribe(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sender, receiver := stream.Pipe[models.Event](1)
		us := NewUpdateService(ctx, receiver)

		ch, unsub := us.subscribe(ctx, nil, nil)

		// Verify the subscriber receives events before unsubscribing.
		ev1 := models.Event{Did: "did:plc:testuser"}
		require.NoError(t, sender.Send(ctx, ev1))
		synctest.Wait()
		require.Equal(t, ev1.Did, (<-ch).Did)

		// Unsubscribe and verify it is removed from the set.
		unsub()
		us.mu.RLock()
		require.Equal(t, 0, len(us.subscribers))
		us.mu.RUnlock()

		// Send another event and verify nothing arrives on the now-removed subscriber's channel.
		ev2 := models.Event{Did: "did:plc:other"}
		require.NoError(t, sender.Send(ctx, ev2))
		synctest.Wait()
		require.Equal(t, 0, len(ch))
	})
}

func TestUpdateServiceMultipleSubscribers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sender, receiver := stream.Pipe[models.Event](1)
		us := NewUpdateService(ctx, receiver)

		ch1, unsub1 := us.subscribe(ctx, nil, nil)
		ch2, unsub2 := us.subscribe(ctx, nil, nil)
		defer unsub1()
		defer unsub2()

		received := make(chan *models.Event, 2)
		go func() { received <- <-ch1 }()
		go func() { received <- <-ch2 }()

		ev := models.Event{Did: "did:plc:testuser"}
		require.NoError(t, sender.Send(ctx, ev))

		// listenForUpdates delivers the event to both buffered channels; readers collect into received.
		synctest.Wait()

		got1 := <-received
		got2 := <-received
		require.Equal(t, ev.Did, got1.Did)
		require.Equal(t, ev.Did, got2.Did)
	})
}
