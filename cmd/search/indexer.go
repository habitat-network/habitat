package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/habitat-network/habitat/internal/events"
	"gorm.io/gorm"
)

type searchCursor struct {
	ID      uint `gorm:"primaryKey"`
	LastSeq uint64
}

func (searchCursor) TableName() string { return "search_cursor" }

type Indexer struct {
	db     *gorm.DB
	index  Index
	source PearClient
}

func NewIndexer(db *gorm.DB, index Index, source PearClient) (*Indexer, error) {
	if err := db.AutoMigrate(&searchCursor{}); err != nil {
		return nil, fmt.Errorf("migrate search_cursor: %w", err)
	}
	return &Indexer{db: db, index: index, source: source}, nil
}

func (ix *Indexer) lastSeq(ctx context.Context) (uint64, error) {
	var cur searchCursor
	err := ix.db.WithContext(ctx).First(&cur, "id = ?", 1).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return cur.LastSeq, nil
}

func (ix *Indexer) saveSeq(ctx context.Context, seq uint64) error {
	return ix.db.WithContext(ctx).Save(&searchCursor{ID: 1, LastSeq: seq}).Error
}

// Run connects to the event source and indexes every event, resuming from
// the last persisted cursor. Blocks until ctx is canceled.
func (ix *Indexer) Run(ctx context.Context) error {
	since, err := ix.lastSeq(ctx)
	if err != nil {
		return fmt.Errorf("load cursor: %w", err)
	}
	return ix.source.SubscribeSpaces(ctx, since, func(event events.Event) {
		if err := ix.handleEvent(ctx, event); err != nil {
			slog.ErrorContext(ctx, "failed to index event", "err", err, "seq", event.Seq)
			return
		}
		if err := ix.saveSeq(ctx, event.Seq); err != nil {
			slog.ErrorContext(ctx, "failed to save cursor", "err", err, "seq", event.Seq)
		}
	})
}

func (ix *Indexer) handleEvent(ctx context.Context, event events.Event) error {
	orgDID := event.Space.SpaceOwner()
	for _, op := range event.Ops {
		if op.Action == "delete" {
			if err := ix.index.Delete(ctx, op.Uri); err != nil {
				return err
			}
			continue
		}
		doc := Document{
			URI:        op.Uri,
			SpaceURI:   event.Space,
			OrgDID:     orgDID,
			Collection: op.Uri.Collection(),
			Content:    ExtractContent(op.Value),
			UpdatedAt:  time.Now(),
		}
		if err := ix.index.Upsert(ctx, doc); err != nil {
			return err
		}
	}
	return nil
}
