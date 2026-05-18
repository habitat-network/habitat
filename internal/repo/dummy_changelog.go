package repo

import (
	"encoding/json"
	"time"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
)

type dummyChangelog struct{}

// Consume implements EventProvider.
func (d *dummyChangelog) Consume() (stream.Stream[models.Event], error) {
	return nil, nil
}

// EmitChangeEvent implements EventEmitter.
func (d *dummyChangelog) EmitChangeEvent(
	did string,
	collection string,
	rkey string,
	op operation,
	ts time.Time,
	record json.RawMessage,
) {
}

func NewDummyChangelog() *dummyChangelog {
	return &dummyChangelog{}
}

var _ EventEmitter = &dummyChangelog{}
var _ EventProvider = &dummyChangelog{}
