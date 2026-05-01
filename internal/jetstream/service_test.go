package jetstream

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/stretchr/testify/require"
)

func TestUpdateServiceSingleSubscriber(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sender, receiver := stream.Pipe[models.Event](1)
		us := NewUpdateService(ctx, receiver)

		ch := us.subscribe(ctx, nil, nil)

		ev := models.Event{Did: "did:plc:testuser"}
		require.NoError(t, sender.Send(ctx, ev))

		// listenForUpdates receives the event and blocks sending to the unbuffered ch.
		synctest.Wait()

		got := <-ch
		require.Equal(t, ev.Did, got.Did)
	})
}

func TestUpdateServiceMultipleSubscribers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sender, receiver := stream.Pipe[models.Event](1)
		us := NewUpdateService(ctx, receiver)

		ch1 := us.subscribe(ctx, nil, nil)
		ch2 := us.subscribe(ctx, nil, nil)

		received := make(chan *models.Event, 2)
		go func() { received <- <-ch1 }()
		go func() { received <- <-ch2 }()

		ev := models.Event{Did: "did:plc:testuser"}
		require.NoError(t, sender.Send(ctx, ev))

		// listenForUpdates delivers the event to both subscribers; readers collect into received.
		synctest.Wait()

		got1 := <-received
		got2 := <-received
		require.Equal(t, ev.Did, got1.Did)
		require.Equal(t, ev.Did, got2.Did)
	})
}
