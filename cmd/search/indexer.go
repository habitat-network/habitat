package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/habitat-network/habitat/internal/sap"
)

const indexerBatchSize = 50

// Indexer drains a sap.Outbox and feeds an Index, so the search index
// tracks every record sap has synced for the org (backfill and live
// updates alike, since both flow through the same outbox).
type Indexer struct {
	index  Index
	outbox sap.Outbox
}

func NewIndexer(index Index, outbox sap.Outbox) *Indexer {
	return &Indexer{index: index, outbox: outbox}
}

// Run drains the outbox until ctx is canceled, blocking on outbox.Watch()
// whenever there are no pending messages.
func (ix *Indexer) Run(ctx context.Context) error {
	for {
		msgs, err := ix.outbox.Poll(ctx, indexerBatchSize)
		if err != nil {
			return fmt.Errorf("poll outbox: %w", err)
		}
		if len(msgs) == 0 {
			select {
			case <-ix.outbox.Watch():
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		for _, msg := range msgs {
			if err := ix.handleMessage(ctx, msg); err != nil {
				slog.ErrorContext(ctx, "failed to index message", "err", err, "uri", msg.URI)
				continue
			}
			if err := msg.Ack(ctx); err != nil {
				slog.ErrorContext(ctx, "failed to ack outbox message", "err", err, "uri", msg.URI)
			}
		}
	}
}

func (ix *Indexer) handleMessage(ctx context.Context, msg sap.OutboxMessage) error {
	if isDeleted(msg.Value) {
		return ix.index.Delete(ctx, msg.URI)
	}
	var value map[string]any
	if err := json.Unmarshal(msg.Value, &value); err != nil {
		return fmt.Errorf("unmarshal record value: %w", err)
	}
	doc := Document{
		URI:        msg.URI,
		SpaceURI:   msg.URI.SpaceURI(),
		OrgDID:     msg.URI.SpaceOwner(),
		Collection: msg.URI.Collection(),
		Content:    ExtractContent(value),
		UpdatedAt:  time.Now(),
	}
	return ix.index.Upsert(ctx, doc)
}

// isDeleted reports whether an outbox message's value represents a
// deletion: sap's writeEventOps marshals a nil record value (set on delete
// ops) to JSON "null".
func isDeleted(value json.RawMessage) bool {
	return len(value) == 0 || string(value) == "null"
}
