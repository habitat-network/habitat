package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Sequencer assigns sequence numbers to events, persists them to the outbox,
// and then fans them out to live subscribers.
type Sequencer struct {
	store  EventAdder
	fanout *Fanout
}

type EventAdder interface {
	AddEvent(ctx context.Context, t time.Time, eventJSON []byte) (int64, error)
}

func NewSequencer(store EventAdder, fanout *Fanout) *Sequencer {
	return &Sequencer{
		store:  store,
		fanout: fanout,
	}
}

func (s *Sequencer) Publish(ctx context.Context, ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	seq, err := s.store.AddEvent(ctx, ev.Time, data)
	if err != nil {
		return fmt.Errorf("add event to store: %w", err)
	}

	ev.Seq = seq
	return s.fanout.Publish(ctx, ev)
}

var _ Publisher = (*Sequencer)(nil)
