package sync

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockEventStore struct {
	events []Event
	seq    int64
}

func (m *mockEventStore) AddEvent(ctx context.Context, t time.Time, eventJSON []byte) (int64, error) {
	m.seq++
	return m.seq, nil
}

func TestSequencer_Publish(t *testing.T) {
	store := &mockEventStore{}
	fanout := NewFanout()
	seq := NewSequencer(store, fanout)

	ch, done := fanout.Subscribe(10)
	defer fanout.Unsubscribe(done)

	ev := Event{
		Time: time.Now(),
		Type: EventSpace,
		Rev:  "123",
	}

	err := seq.Publish(context.Background(), ev)
	require.NoError(t, err)

	select {
	case received := <-ch:
		assert.Equal(t, int64(1), received.Seq)
		assert.Equal(t, ev.Rev, received.Rev)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}

	err = seq.Publish(context.Background(), ev)
	require.NoError(t, err)

	select {
	case received := <-ch:
		assert.Equal(t, int64(2), received.Seq)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}
