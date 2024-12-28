package pubsub

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type TestEvent struct {
	shouldError bool
}

type TestSubscriber struct {
	consumedEvents []*TestEvent
}

func (s *TestSubscriber) Name() string {
	return "TestSubscriber"
}

func (s *TestSubscriber) ConsumeEvent(e *TestEvent) error {
	if e == nil {
		return errors.New("No nil events allowed")
	}
	s.consumedEvents = append(s.consumedEvents, e)
	return nil
}

func TestSimpleChannel(t *testing.T) {
	subscriber1 := &TestSubscriber{
		consumedEvents: make([]*TestEvent, 0),
	}
	subscriber2 := &TestSubscriber{
		consumedEvents: make([]*TestEvent, 0),
	}

	sp := NewSimplePublisher[*TestEvent]()

	channel := NewSimpleChannel[*TestEvent]([]Publisher[*TestEvent]{sp}, []Subscriber[*TestEvent]{subscriber1, subscriber2})
	go func() {
		err := channel.Listen()
		if err != nil {
			t.Error(err)
		}
	}()

	err := sp.PublishEvent(&TestEvent{shouldError: false})
	assert.Nil(t, err)

	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, len(subscriber1.consumedEvents), 1)
	assert.Equal(t, len(subscriber2.consumedEvents), 1)

	err = sp.PublishEvent(&TestEvent{shouldError: true})
	assert.Nil(t, err)

	assert.Equal(t, len(subscriber1.consumedEvents), 1)
	assert.Equal(t, len(subscriber2.consumedEvents), 1)
}
