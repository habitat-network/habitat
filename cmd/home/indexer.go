package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/habitat-network/habitat/internal/sap"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const indexerBatchSize = 50

// Indexer drains sap's outbox and maintains the home server's group index:
// network.habitat.group.profile records become group rows and
// network.habitat.relationship.tuple records granting a role on a group-space
// become tuple rows. Both backfill and live firehose updates flow through the
// same outbox, so the index tracks every group the org syncs.
type Indexer struct {
	store  *Store
	outbox sap.Outbox
}

func NewIndexer(store *Store, outbox sap.Outbox) *Indexer {
	return &Indexer{store: store, outbox: outbox}
}

// Run drains the outbox until ctx is canceled, blocking on outbox.Watch() when
// there are no pending messages.
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
			if err := ix.handle(ctx, msg); err != nil {
				slog.ErrorContext(ctx, "failed to index message", "err", err, "uri", msg.URI)
				continue
			}
			if err := ix.outbox.Ack(ctx, msg.ID); err != nil {
				slog.ErrorContext(ctx, "failed to ack message", "err", err, "uri", msg.URI)
			}
		}
	}
}

func (ix *Indexer) handle(ctx context.Context, msg sap.OutboxMessage) error {
	deleted := isDeleted(msg.Value)

	// Every synced record is indexed into the records table so the collections
	// endpoints can browse the org's data by collection type, regardless of
	// whether the collection also feeds the group index below.
	if deleted {
		if err := ix.store.DeleteRecord(ctx, msg.URI); err != nil {
			return err
		}
	} else if err := ix.store.UpsertRecord(ctx, msg.URI); err != nil {
		return err
	}

	switch msg.URI.Collection().String() {
	case collectionGroupProfile:
		if deleted {
			return ix.store.DeleteProfile(ctx, msg.URI)
		}
		return ix.indexProfile(ctx, msg)
	case collectionTuple:
		if deleted {
			return ix.store.DeleteTuple(ctx, msg.URI)
		}
		return ix.indexTuple(ctx, msg)
	default:
		return nil
	}
}

func (ix *Indexer) indexProfile(ctx context.Context, msg sap.OutboxMessage) error {
	var profile struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		CreatedAt   string `json:"createdAt"`
	}
	if err := json.Unmarshal(msg.Value, &profile); err != nil {
		return fmt.Errorf("unmarshal group profile: %w", err)
	}
	return ix.store.UpsertProfile(
		ctx,
		msg.URI,
		profile.Name,
		profile.Description,
		profile.CreatedAt,
	)
}

func (ix *Indexer) indexTuple(ctx context.Context, msg sap.OutboxMessage) error {
	row, ok, err := parseTuple(msg.URI, msg.Value)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	return ix.store.UpsertTuple(ctx, row)
}

// parseTuple decodes a relationship tuple record into a tupleRow. ok is false
// (with no error) for tuples that don't grant a role on a group-space, which
// the group index ignores.
func parseTuple(uri habitat_syntax.SpaceRecordURI, value json.RawMessage) (tupleRow, bool, error) {
	var rec struct {
		Subject  map[string]any `json:"subject"`
		Relation string         `json:"relation"`
		Object   struct {
			Space string `json:"space"`
		} `json:"object"`
	}
	if err := json.Unmarshal(value, &rec); err != nil {
		return tupleRow{}, false, fmt.Errorf("unmarshal tuple: %w", err)
	}

	object, err := habitat_syntax.ParseSpaceURI(rec.Object.Space)
	if err != nil {
		return tupleRow{}, false, nil
	}
	if object.SpaceType().String() != GroupSpaceType {
		return tupleRow{}, false, nil
	}

	row := tupleRow{
		RecordURI:   uri.String(),
		ObjectSpace: object.String(),
		Relation:    rec.Relation,
	}
	switch rec.Subject["$type"] {
	case "network.habitat.relationship.defs#userSubject":
		did, _ := rec.Subject["did"].(string)
		row.SubjectKind = "user"
		row.SubjectDID = did
	case "network.habitat.relationship.defs#spaceRoleSubject":
		space, _ := rec.Subject["space"].(string)
		subRole, _ := rec.Subject["role"].(string)
		row.SubjectKind = "group"
		row.SubjectGroup = space
		row.SubjectRole = subRole
	default:
		return tupleRow{}, false, nil
	}
	return row, true, nil
}

// isDeleted reports whether an outbox message represents a deletion: sap
// marshals a nil record value to JSON "null".
func isDeleted(value json.RawMessage) bool {
	return len(value) == 0 || string(value) == "null"
}
