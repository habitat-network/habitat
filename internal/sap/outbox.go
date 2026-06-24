package sap

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
	"gorm.io/gorm"
)

func writeEventOps(tx *gorm.DB, ops []events.EventOps) error {
	for _, op := range ops {
		value, err := json.Marshal(op.Value)
		if err != nil {
			return err
		}
		if err := tx.Create(&outbox{
			URI:   op.Uri,
			Value: value,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func writeOplogRecords(
	tx *gorm.DB,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	records []habitat.NetworkHabitatSpaceGetRepoOplogRecord,
) error {
	for _, rec := range records {
		collection, err := syntax.ParseNSID(rec.Collection)
		if err != nil {
			return fmt.Errorf("parse collection %q: %w", rec.Collection, err)
		}
		rkey, err := syntax.ParseRecordKey(rec.Rkey)
		if err != nil {
			return fmt.Errorf("parse rkey %q: %w", rec.Rkey, err)
		}
		value, err := json.Marshal(rec.Value)
		if err != nil {
			return err
		}
		uri := habitat_syntax.ConstructSpaceRecordURI(space, repo, collection, rkey)
		if err := tx.Create(&outbox{
			URI:   uri,
			Value: value,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

// OutboxMessage is a single event delivered from the outbox. Ack must be
// called once the message has been durably processed; until then it will
// be redelivered by Poll.
type OutboxMessage struct {
	ID    uint
	URI   habitat_syntax.SpaceRecordURI
	Value json.RawMessage

	ack func(ctx context.Context) error
}

// Ack marks the message as processed so it is not redelivered by Poll.
func (m OutboxMessage) Ack(ctx context.Context) error {
	return m.ack(ctx)
}

// Outbox exposes durable, ordered delivery of repo events to consumers.
// Messages are redelivered by Poll until acknowledged.
type Outbox interface {
	// Poll returns up to limit unacknowledged messages in delivery order.
	Poll(ctx context.Context, limit int) ([]OutboxMessage, error)
	// Watch returns a channel that is notified when new messages may be
	// available to poll. It is a single shared hint, not a per-caller
	// fan-out: only one consumer should be draining it at a time.
	Watch() <-chan struct{}
}

type outboxImpl struct {
	db     *gorm.DB
	notify *utils.PollNotifier
}

func newOutbox(db *gorm.DB, notify *utils.PollNotifier) *outboxImpl {
	return &outboxImpl{db: db, notify: notify}
}

// Poll implements [Outbox].
func (o *outboxImpl) Poll(ctx context.Context, limit int) ([]OutboxMessage, error) {
	var rows []outbox
	if err := o.db.WithContext(ctx).
		Where("acked_at IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("poll outbox: %w", err)
	}

	msgs := make([]OutboxMessage, len(rows))
	for i, row := range rows {
		id := row.ID
		msgs[i] = OutboxMessage{
			ID:    id,
			URI:   row.URI,
			Value: json.RawMessage(row.Value),
			ack: func(ctx context.Context) error {
				return o.db.WithContext(ctx).
					Model(&outbox{}).
					Where("id = ?", id).
					Update("acked_at", time.Now()).Error
			},
		}
	}
	return msgs, nil
}

// Watch implements [Outbox].
func (o *outboxImpl) Watch() <-chan struct{} {
	return o.notify.Listen()
}
