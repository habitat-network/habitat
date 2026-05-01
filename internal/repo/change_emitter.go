package repo

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/rs/zerolog/log"
)

type changeEmitter struct {
	ctx context.Context

	// Only one receiver is supported
	consumed atomic.Bool

	// Sender end of the buffered pipe
	sender *stream.PipeSender[models.Event]

	// Receiver end of the buffered pipe: only one receiver is supported
	receiver stream.Stream[models.Event]
}

type ChangeEmitter interface {
	// Consumers call consume to receive a stream they can listen for new events on
	Consume() (stream.Stream[models.Event], error)
}

func newChangeEmitter(ctx context.Context, bufferSize int) *changeEmitter {
	// bufferSize = how many messages to buffer if the consumer is slow to read
	// If the buffer is full, send the consumer away and recreate the sender + receiver TODO: lock
	sender, receiver := stream.Pipe[models.Event](bufferSize)

	return &changeEmitter{
		ctx:      ctx,
		sender:   sender,
		receiver: receiver,
	}
}

// Consume implements stem.Stem.
func (c *changeEmitter) Consume() (stream.Stream[models.Event], error) {
	if !c.consumed.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("only one consumer allowed for now")
	}

	return c.receiver, nil
}

type operation string

const (
	OperationCreate operation = "create"
	OperationUpdate operation = "update"
	OperationDelete operation = "delete"
)

// EmitChangeEvent emits a jetstream event based on the given change data
// Right now the change emitter relies on some caller to explicitly write out every change. In the future it could tail
// a database WAL or something else similar.
func (c *changeEmitter) EmitChangeEvent(did, collection, rkey string, op operation, ts time.Time, record json.RawMessage) {
	if !c.consumed.Load() {
		return
	}

	ev := models.Event{
		Did:    did,
		TimeUS: ts.UnixNano(),
		Kind:   models.EventKindCommit,
		Commit: &models.Commit{
			Operation:  string(op),
			Collection: collection,
			Rev:        rkey,
			Record:     record,
		},
	}

	err := c.sender.Send(c.ctx, ev)
	// TODO handle error
	// For now long the error and move on
	if err != nil {
		log.Err(err).Msg("error emitting change event")
	}
}
