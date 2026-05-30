package sync

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFanoutPublishSubscribe(t *testing.T) {
	f := NewFanout()
	ch, done := f.Subscribe(10)
	defer f.Unsubscribe(done)

	ctx := context.Background()
	event := Event{Rev: "test-rev", Type: EventSpaceRecord}

	err := f.Publish(ctx, event)
	assert.NoError(t, err)

	select {
	case received := <-ch:
		assert.Equal(t, int64(1), received.Seq)
		assert.Equal(t, event.Rev, received.Rev)
		assert.Equal(t, event.Type, received.Type)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestFanoutMultipleSubscribers(t *testing.T) {
	f := NewFanout()
	ch1, done1 := f.Subscribe(10)
	defer f.Unsubscribe(done1)
	ch2, done2 := f.Subscribe(10)
	defer f.Unsubscribe(done2)

	ctx := context.Background()
	event := Event{Rev: "multi-rev", Type: EventSpaceMember}

	err := f.Publish(ctx, event)
	assert.NoError(t, err)

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case received := <-ch:
			assert.Equal(t, int64(1), received.Seq)
			assert.Equal(t, event.Rev, received.Rev)
			assert.Equal(t, event.Type, received.Type)
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d timeout", i)
		}
	}
}

func TestFanoutSlowSubscriber(t *testing.T) {
	f := NewFanout()
	ch, done := f.Subscribe(1) // buffer of 1
	defer f.Unsubscribe(done)

	ctx := context.Background()

	// Fill the buffer
	f.Publish(ctx, Event{Rev: "first"})
	// This one should be dropped (buffer full)
	err := f.Publish(ctx, Event{Rev: "second"})
	assert.NoError(t, err) // Publish never returns error for slow subscribers

	// Should receive first event, second was dropped
	select {
	case received := <-ch:
		assert.Equal(t, "first", received.Rev)
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for first event")
	}

	select {
	case <-ch:
		t.Fatal("should not have received second event (dropped)")
	case <-time.After(100 * time.Millisecond):
		// expected
	}
}

func TestFanoutSubscriberCount(t *testing.T) {
	f := NewFanout()
	assert.Equal(t, 0, f.SubscriberCount())

	_, done1 := f.Subscribe(10)
	assert.Equal(t, 1, f.SubscriberCount())

	_, done2 := f.Subscribe(10)
	assert.Equal(t, 2, f.SubscriberCount())

	f.Unsubscribe(done1)
	assert.Equal(t, 1, f.SubscriberCount())

	f.Unsubscribe(done2)
	assert.Equal(t, 0, f.SubscriberCount())
}

func TestFanoutUnsubscribeClosesChannels(t *testing.T) {
	f := NewFanout()
	ch, done := f.Subscribe(10)

	f.Unsubscribe(done)

	// Channel should be closed
	_, ok := <-ch
	assert.False(t, ok, "channel should be closed after unsubscribe")
}

func TestNopPublisher(t *testing.T) {
	p := &NopPublisher{}
	err := p.Publish(context.Background(), Event{Rev: "test"})
	assert.NoError(t, err)
}

func TestFanoutConcurrentPublishSubscribe(t *testing.T) {
	f := NewFanout()

	ch, done := f.Subscribe(100)
	defer f.Unsubscribe(done)

	ctx := context.Background()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			f.Publish(ctx, Event{Rev: fmt.Sprintf("rev-%d", n)})
		}(i)
	}
	wg.Wait()

	count := 0
	for {
		select {
		case <-ch:
			count++
		case <-time.After(500 * time.Millisecond):
			require.Equal(t, 50, count, "all 50 events should be received")
			return
		}
	}
}

func TestFanoutUnsubscribeDuringPublish(t *testing.T) {
	f := NewFanout()
	ctx := context.Background()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			f.Publish(ctx, Event{Rev: fmt.Sprintf("bg-%d", i)})
		}
		close(done)
	}()

	for i := 0; i < 10; i++ {
		_, d := f.Subscribe(10)
		f.Unsubscribe(d)
	}
	<-done
}

func TestFanoutDoubleUnsubscribe(t *testing.T) {
	f := NewFanout()
	_, done := f.Subscribe(10)

	f.Unsubscribe(done)
	f.Unsubscribe(done)
}

func TestFanoutSubscribeUnbuffered(t *testing.T) {
	f := NewFanout()
	ch, done := f.Subscribe(0)
	defer f.Unsubscribe(done)

	ctx := context.Background()

	go func() {
		time.Sleep(50 * time.Millisecond)
		<-ch
	}()

	err := f.Publish(ctx, Event{Rev: "unbuffered"})
	assert.NoError(t, err)
}
