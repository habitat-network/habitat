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

func (us *updateService) subscribe(ctx context.Context, collections, dids []string) chan *models.Event {
	sub := &subscriber{
		ctx:         ctx,
		ch:          make(chan *models.Event),
		collections: collections,
		dids:        dids,
	}
	// add sub to subscribers
	us.mu.Lock()
	defer us.mu.Unlock()
	us.subscribers.Add(sub)
	return sub.ch
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

		us.mu.RLock()
		// TODO: don't hold lock while iterating over this
		for sub, _ := range us.subscribers {
			sub.send(&ev)
		}
		us.mu.RUnlock()
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

func (s *subscriber) send(ev *models.Event) {
	if s.wants(ev) {
		select {
		case <-s.ctx.Done():
			return
		case s.ch <- ev:
			// TODO: should we timeout sending since this is centrally blocking?
		}
	}
}
