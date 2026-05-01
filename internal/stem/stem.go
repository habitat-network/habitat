package stem

// This package contains a service, stem, which ingests row events from the database and outputs structured events
// that can be consumed by other services.
// stem is an acronym for: STructured Event Mapper.

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
)

type Stem interface {
	// Consumers call consume to receive a stream they can listen for new events on
	Consume() (stream.Stream[models.Event], error)
}

type stem struct {
	ctx context.Context

	// Sender end of the buffered pipe
	sender *stream.PipeSender[models.Event]

	// Receiver end of the buffered pipe: only one receiver is supported
	consumed atomic.Bool
	receiver stream.Stream[models.Event]
}

// Consume implements Stem.
func (s *stem) Consume() (stream.Stream[models.Event], error) {
	if !s.consumed.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("only one consumer allowed for now")
	}

	return s.receiver, nil
}

var _ Stem = &stem{}

func New(ctx context.Context, bufferSize int) Stem {
	sender, receiver := stream.Pipe[models.Event](bufferSize)
	return &stem{
		sender:   sender,
		receiver: receiver,
	}
}

// Probably defined elsewhere: this is a repo change that gets turned into jetstream event
type RepoChange struct{}

func changeToEvent(change RepoChange) models.Event {
	return models.Event{}
}

func (s *stem) listenAndStream(ch chan RepoChange) {
	// Listen to changes on ch and send them to consumer.
	for {
		select {
		case <-s.ctx.Done():
			return
		case msg := <-ch: // TODO can ch be closed?
			ev := changeToEvent(msg)
			err := s.sender.Send(s.ctx, ev)
		}
	}
}
