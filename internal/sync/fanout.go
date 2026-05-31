package sync

import (
	"context"
	"sync"

	"github.com/rs/zerolog/log"
)

type subscriber struct {
	ch   chan Event
	done chan struct{}
}

type Fanout struct {
	mu          sync.RWMutex
	subscribers map[*subscriber]struct{}
}

func NewFanout() *Fanout {
	return &Fanout{
		subscribers: make(map[*subscriber]struct{}),
	}
}

func (f *Fanout) Subscribe(buffer int) (<-chan Event, chan struct{}) {
	sub := &subscriber{ch: make(chan Event, buffer), done: make(chan struct{})}
	f.mu.Lock()
	f.subscribers[sub] = struct{}{}
	f.mu.Unlock()
	return sub.ch, sub.done
}

func (f *Fanout) Unsubscribe(done chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for sub := range f.subscribers {
		if sub.done == done {
			delete(f.subscribers, sub)
			close(sub.done)
			close(sub.ch)
			return
		}
	}
}

func (f *Fanout) Publish(ctx context.Context, event Event) error {
	f.mu.RLock()
	defer f.mu.RUnlock()
	for sub := range f.subscribers {
		select {
		case sub.ch <- event:
		default:
			log.Ctx(ctx).Warn().
				Str("event_type", string(event.Type)).
				Str("rev", event.Rev).
				Int64("seq", event.Seq).
				Msg("dropping event for slow subscriber")
		}
	}
	return nil
}

func (f *Fanout) SubscriberCount() int {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return len(f.subscribers)
}

var _ Publisher = (*Fanout)(nil)
