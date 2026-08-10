// Package outbox provides durable, acknowledged delivery of synced records to
// the library's consumer. The sync engine emits records here; the consumer
// polls, processes, and acks them. Unacked messages are redelivered.
package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

// record is a single emitted record awaiting acknowledgement.
type record struct {
	ID        uint `gorm:"primaryKey;autoIncrement"`
	URI       habitat_syntax.SpaceRecordURI
	Value     []byte
	CreatedAt time.Time
	AckedAt   *time.Time
}

func (record) TableName() string { return "sap_outbox" }

// AutoMigrate creates the outbox table.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(&record{})
}

// Message is a single event delivered from the outbox. Ack must be called with
// the message's ID once it has been durably processed; until then it is
// redelivered by Poll.
type Message struct {
	ID    uint
	URI   habitat_syntax.SpaceRecordURI
	Value json.RawMessage
}

// Outbox is the consumer-facing read side: ordered delivery with
// redelivery-until-ack.
type Outbox interface {
	// Poll returns up to limit unacknowledged messages in delivery order.
	Poll(ctx context.Context, limit int) ([]Message, error)
	// Ack marks the message with the given ID as processed.
	Ack(ctx context.Context, id uint) error
	// Watch returns a channel notified when new messages may be available. It
	// is a single shared hint, not a per-caller fan-out: only one consumer
	// should drain it at a time.
	Watch() <-chan struct{}
}

// Store is the outbox backed by the sap database. It is both the Emitter used
// by the sync engine and the Outbox read by the consumer.
type Store struct {
	db     *gorm.DB
	notify *utils.PollNotifier
}

var _ Outbox = (*Store)(nil)

func NewStore(db *gorm.DB, notify *utils.PollNotifier) *Store {
	return &Store{db: db, notify: notify}
}

// WithTx returns a Store scoped to the given transaction, so emits can join a
// caller's transaction.
func (s *Store) WithTx(tx *gorm.DB) *Store {
	return &Store{db: tx, notify: s.notify}
}

// Emit appends a record for delivery and wakes the watcher.
func (s *Store) Emit(
	ctx context.Context,
	uri habitat_syntax.SpaceRecordURI,
	value []byte,
) error {
	if err := s.db.WithContext(ctx).Create(&record{URI: uri, Value: value}).Error; err != nil {
		return fmt.Errorf("emit record: %w", err)
	}
	s.notify.Notify()
	return nil
}

// Poll implements [Outbox].
func (s *Store) Poll(ctx context.Context, limit int) ([]Message, error) {
	var rows []record
	if err := s.db.WithContext(ctx).
		Where("acked_at IS NULL").
		Order("id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("poll outbox: %w", err)
	}

	msgs := make([]Message, len(rows))
	for i, row := range rows {
		msgs[i] = Message{
			ID:    row.ID,
			URI:   row.URI,
			Value: json.RawMessage(row.Value),
		}
	}
	return msgs, nil
}

// Ack implements [Outbox].
func (s *Store) Ack(ctx context.Context, id uint) error {
	return s.db.WithContext(ctx).
		Model(&record{}).
		Where("id = ?", id).
		Update("acked_at", time.Now()).Error
}

// Watch implements [Outbox].
func (s *Store) Watch() <-chan struct{} {
	return s.notify.Listen()
}
