package jetstream

import (
	"context"
	"sync"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/bradenaw/juniper/xmaps"
)

type updateService struct {
	mu sync.RWMutex // protects everything below
	// For now simple set; on each change iterate through and see who wants it
	// Eventually, maintain an index for easy look-ups on collection --> sub / did --> sub
	subscribers xmaps.Set[*subscriber]
}

func NewUpdateService(ctx context.Context, in stream.Stream[models.Event]) *updateService {
	us := &updateService{
		mu:          sync.RWMutex{},
		subscribers: make(xmaps.Set[*subscriber]),
	}
	// TODO: probably use errgroup here
	go func() {
		us.listenForUpdates(ctx, in)
	}()
	return us
}

const (
	// Add a small buffer to the channel to manage slowdowns
	subscriberBufferSize = 500
)

func (us *updateService) subscribe(
	ctx context.Context,
	collections, dids []string,
) (ch chan *models.Event, cancel func()) {
	sub := &subscriber{
		ctx:         ctx,
		ch:          make(chan *models.Event, subscriberBufferSize),
		collections: collections,
		dids:        dids,
	}
	// add sub to subscribers
	us.mu.Lock()
	defer us.mu.Unlock()
	us.subscribers.Add(sub)
	return sub.ch, func() {
		us.unsubscribe(sub)
	}
}

func (us *updateService) unsubscribe(sub *subscriber) {
	us.mu.Lock()
	defer us.mu.Unlock()
	us.subscribers.Remove(sub)
}

func (us *updateService) listenForUpdates(ctx context.Context, in stream.Stream[models.Event]) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		ev, err := in.Next(ctx)
		if err != nil {
			return
		}

		// TODO: there's a better way of handling slow subscribers + locking
		toClose := make(xmaps.Set[*subscriber])
		us.mu.RLock()
		for sub := range us.subscribers {
			blocked := sub.send(&ev) // TODO: do something here if the sender is blocked
			if blocked {
				toClose.Add(sub)
			}
		}
		us.mu.RUnlock()

		if len(toClose) > 0 {
			us.mu.Lock()
			for sub := range toClose {
				close(
					sub.ch,
				) // Signals to the server that this subscriber needs to go away and come back
				us.subscribers.Remove(sub)
			}
			us.mu.Unlock()
		}
	}
}

type subscriber struct {
	ctx context.Context
	ch  chan *models.Event

	collections []string
	dids        []string
}

func (s *subscriber) wants(ev *models.Event) bool {
	return true // TODO: logic for filtering based on desired collections / dids / etc.
}

// Returns whether the send was successful
func (s *subscriber) send(ev *models.Event) (blocked bool) {
	if s.wants(ev) {
		select {
		case <-s.ctx.Done():
			return false
		case s.ch <- ev:
			// TODO: should we timeout sending since this is centrally blocking
			return false
		default:
			return true
		}
	}
	return false
}
