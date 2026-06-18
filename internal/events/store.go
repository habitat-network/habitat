package events

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"gorm.io/gorm"
)

type eventEntry struct {
	gorm.Model
	Tid       syntax.TID `gorm:"uniqueIndex"`
	Seq       *uint64    `gorm:"uniqueIndex"`
	Event     []byte
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Event struct {
	Seq   uint64
	Type  string
	Space habitat_syntax.SpaceURI
	Time  time.Time
	Repo  syntax.DID
	Rev   syntax.TID `json:"rev,omitempty"`
	Since syntax.TID `json:"since,omitempty"`
	Ops   []EventOps
}

type EventOps struct {
	Action string

	// RepoOp
	Uri   habitat_syntax.SpaceRecordURI
	Value map[string]any
	Cid   string

	// Membership
	Did    syntax.DID `json:"did,omitempty"`
	Access string
}

// EventStream returns a channel of sequenced event objects
type EventStream interface {
	Subscribe(ctx context.Context, since uint64) <-chan Event
}

// Store appends to the event store, sequences events, and pushes events to subscribers
type Store interface {
	EventStream
	AppendSpaceEvent(
		ctx context.Context,
		space habitat_syntax.SpaceURI,
		repo syntax.DID,
		rev syntax.TID,
		prev syntax.TID,
		ops []EventOps,
	) error
	StartSequencer(ctx context.Context) error
	// NotifyEvent should be called after appending an event succeeds in order to sequence the event
	NotifyEvent(ctx context.Context)

	db.Store[Store]
}

type storeImpl struct {
	db            *gorm.DB
	seqCh         chan struct{}
	subscribersMu *sync.RWMutex
	subscribers   map[chan struct{}]struct{}
}

func NewStore(db *gorm.DB) (Store, error) {
	err := db.AutoMigrate(&eventEntry{})
	if err != nil {
		return nil, err
	}
	seqCh := make(chan struct{}, 1)
	seqCh <- struct{}{} // initial notification
	return &storeImpl{
		db:            db,
		seqCh:         seqCh,
		subscribers:   map[chan struct{}]struct{}{},
		subscribersMu: new(sync.RWMutex),
	}, nil
}

// AppendSpaceEvent implements [Store].
func (s *storeImpl) AppendSpaceEvent(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev syntax.TID,
	prev syntax.TID,
	ops []EventOps,
) error {
	entry, err := json.Marshal(Event{
		Type:  "space",
		Space: space,
		Repo:  repo,
		Rev:   rev,
		Since: prev,
		Ops:   ops,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}
	event := eventEntry{Event: entry, Tid: rev}
	result := s.db.WithContext(ctx).Create(&event)
	if result.Error != nil {
		return fmt.Errorf("failed to create event: %w", result.Error)
	}
	return nil
}

// NotifyEvent implements [Store].
func (s *storeImpl) NotifyEvent(ctx context.Context) {
	slog.DebugContext(ctx, "notifying sequencer")
	// notify sequencer
	select {
	case s.seqCh <- struct{}{}:
	default:
		slog.DebugContext(ctx, "sequencer already notified")
		// sequencer already notified. this event will be picked up during next sequencer run
	}
}

// StartSequencer implements [Store].
func (s *storeImpl) StartSequencer(ctx context.Context) error {
	var lastSequencedEvent eventEntry
	err := s.db.Where("seq IS NOT NULL").Order("seq DESC").First(&lastSequencedEvent).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.ErrorContext(ctx, "failed to get last sequenced event", "err", err)
		return err
	}
	nextSeq := uint64(1)
	if lastSequencedEvent.Seq != nil {
		nextSeq = *lastSequencedEvent.Seq + 1
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.seqCh:
			slog.DebugContext(ctx, "sequencing events")
			var entries []eventEntry
			if err := s.db.Model(&eventEntry{}).
				Where("seq IS NULL").
				Order("tid ASC").
				Find(&entries).
				Error; err != nil {
				slog.ErrorContext(ctx, "failed to get unsequenced events", "err", err)
				continue
			}
			for _, entry := range entries {
				if err := s.db.Model(&entry).Update("seq", new(nextSeq)).Error; err != nil {
					slog.ErrorContext(ctx, "failed to update unsequenced event", "err", err)
					continue
				}
				nextSeq++
			}
			s.subscribersMu.RLock()
			for ch := range s.subscribers {
				select {
				case ch <- struct{}{}:
				default:
					// subscriber already notified
				}
			}
			s.subscribersMu.RUnlock()
		}
	}
}

// Subscribe implements [Store].
func (s *storeImpl) Subscribe(ctx context.Context, since uint64) <-chan Event {
	notificationCh := make(chan struct{}, 1)
	s.subscribersMu.Lock()
	s.subscribers[notificationCh] = struct{}{}
	s.subscribersMu.Unlock()

	// send an initial notification
	notificationCh <- struct{}{}

	lastSent := since

	ch := make(chan Event)
	go func() {
		for {
			select {
			case <-ctx.Done():
				s.subscribersMu.Lock()
				delete(s.subscribers, notificationCh)
				s.subscribersMu.Unlock()
				return
			case <-notificationCh:
				slog.DebugContext(ctx, "notifying subscriber")
				events, err := s.GetEvents(ctx, lastSent)
				if err != nil {
					slog.ErrorContext(ctx, "failed to get events", "err", err)
					continue
				}
				for _, event := range events {
					if event.Seq > lastSent {
						select {
						case <-ctx.Done():
							return
						case ch <- event:
						}
						lastSent = event.Seq
					}
				}
			}
		}
	}()

	return ch
}

// GetEvents implements [Store].
func (s *storeImpl) GetEvents(
	ctx context.Context,
	since uint64,
) ([]Event, error) {
	var eventEntries []eventEntry
	if err := s.db.WithContext(ctx).
		Where("seq > ?", since).
		Order("seq ASC").
		Find(&eventEntries).
		Error; err != nil {
		return nil, err
	}
	var events []Event
	for _, entry := range eventEntries {
		var event Event
		if err := json.Unmarshal(entry.Event, &event); err != nil {
			return nil, err
		}
		event.Seq = *entry.Seq
		event.Time = entry.CreatedAt
		events = append(events, event)
	}
	return events, nil
}

// WithTx implements [Store].
func (s *storeImpl) WithTx(tx *gorm.DB) Store {
	return &storeImpl{
		db:            tx,
		seqCh:         s.seqCh,
		subscribers:   s.subscribers,
		subscribersMu: s.subscribersMu,
	}
}
