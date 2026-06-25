package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/sap"
)

const ingestBatchSize = 50

// postNSID is the collection greensky cares about; every other record sap
// syncs is acked and ignored.
var postNSID = syntax.NSID("network.habitat.greensky.post")

// Ingester drains a sap.Outbox and persists greensky posts, so the store
// tracks every post sap has synced for the org (backfill and live updates
// alike, since both flow through the same outbox).
type Ingester struct {
	store  *PostStore
	outbox sap.Outbox
}

func NewIngester(store *PostStore, outbox sap.Outbox) *Ingester {
	return &Ingester{store: store, outbox: outbox}
}

// Run drains the outbox until ctx is canceled, blocking on outbox.Watch()
// whenever there are no pending messages.
func (ix *Ingester) Run(ctx context.Context) error {
	for {
		msgs, err := ix.outbox.Poll(ctx, ingestBatchSize)
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
				slog.ErrorContext(ctx, "failed to ingest message", "err", err, "uri", msg.URI)
				continue
			}
			if err := ix.outbox.Ack(ctx, msg.ID); err != nil {
				slog.ErrorContext(ctx, "failed to ack outbox message", "err", err, "uri", msg.URI)
			}
		}
	}
}

func (ix *Ingester) handleMessage(ctx context.Context, msg sap.OutboxMessage) error {
	if msg.URI.Collection() != postNSID {
		// Not a greensky post; nothing to store, but acking it (by returning
		// nil) keeps the outbox from redelivering forever.
		return nil
	}
	if isDeleted(msg.Value) {
		return ix.store.Delete(ctx, msg.URI.String())
	}

	var rec habitat.NetworkHabitatGreenskyPost
	if err := json.Unmarshal(msg.Value, &rec); err != nil {
		return fmt.Errorf("unmarshal post record: %w", err)
	}

	indexedAt := time.Now()
	postedAt := parseTimestamp(rec.CreatedAt, indexedAt)

	return ix.store.Upsert(ctx, post{
		URI:         msg.URI.String(),
		SpaceURI:    msg.URI.SpaceURI().String(),
		Author:      recordAuthor(msg.URI).String(),
		Text:        rec.Text,
		PostedAt:    postedAt,
		ReplyParent: rec.Reply.Parent,
		ReplyRoot:   rec.Reply.Root,
		IndexedAt:   indexedAt,
	})
}

// parseTimestamp parses a client-declared RFC3339 timestamp, falling back to
// the ingestion time if it's missing or malformed so a bad client can't break
// ordering.
func parseTimestamp(raw string, fallback time.Time) time.Time {
	if raw == "" {
		return fallback
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return fallback
	}
	return t
}

// isDeleted reports whether an outbox message's value represents a deletion:
// sap's writeEventOps marshals a nil record value (set on delete ops) to JSON
// "null".
func isDeleted(value json.RawMessage) bool {
	return len(value) == 0 || string(value) == "null"
}
