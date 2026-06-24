package main

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/habitat-network/habitat/internal/sap"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

// fakeOutbox is an in-memory test double for sap.Outbox: Poll returns a
// fixed batch once, then returns empty and the indexer blocks on Watch
// until the test cancels the context.
type fakeOutbox struct {
	pending []sap.OutboxMessage
	watchCh chan struct{}
}

func newFakeOutbox(msgs []sap.OutboxMessage) *fakeOutbox {
	return &fakeOutbox{pending: msgs, watchCh: make(chan struct{})}
}

func (f *fakeOutbox) Poll(ctx context.Context, limit int) ([]sap.OutboxMessage, error) {
	if len(f.pending) == 0 {
		return nil, nil
	}
	msgs := f.pending
	f.pending = nil
	return msgs, nil
}

func (f *fakeOutbox) Watch() <-chan struct{} {
	return f.watchCh
}

// fakeIndex is an in-memory Index for testing the indexer in isolation.
type fakeIndex struct {
	upserted []Document
	deleted  []string
}

func (f *fakeIndex) Upsert(ctx context.Context, doc Document) error {
	f.upserted = append(f.upserted, doc)
	return nil
}

func (f *fakeIndex) Delete(ctx context.Context, uri habitat_syntax.SpaceRecordURI) error {
	f.deleted = append(f.deleted, uri.String())
	return nil
}

func (f *fakeIndex) Query(ctx context.Context, params QueryParams) (QueryResult, error) {
	return QueryResult{}, nil
}

func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return b
}

func TestIndexer_UpsertsMessageWithValue(t *testing.T) {
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:org1/network.habitat.space/skey1/did:plc:user1/network.habitat.note/rkey1",
	)
	index := &fakeIndex{}
	var acked atomic.Bool
	outbox := newFakeOutbox([]sap.OutboxMessage{
		sap.NewOutboxMessageForTesting(
			1,
			recordURI,
			mustMarshal(t, map[string]any{"title": "Budget"}),
			func(ctx context.Context) error {
				acked.Store(true)
				return nil
			},
		),
	})

	indexer := NewIndexer(index, outbox)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = indexer.Run(ctx)

	require.Len(t, index.upserted, 1)
	require.Equal(t, recordURI, index.upserted[0].URI)
	require.Equal(
		t,
		habitat_syntax.SpaceURI("ats://did:plc:org1/network.habitat.space/skey1"),
		index.upserted[0].SpaceURI,
	)
	require.Equal(t, "did:plc:org1", index.upserted[0].OrgDID.String())
	require.Equal(t, "network.habitat.note", index.upserted[0].Collection.String())
	require.Contains(t, index.upserted[0].Content, "Budget")
	require.True(t, acked.Load(), "message should be acked after a successful upsert")
}

func TestIndexer_DeletesOnNullValue(t *testing.T) {
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:org1/network.habitat.space/skey1/did:plc:user1/network.habitat.note/rkey1",
	)
	index := &fakeIndex{}
	outbox := newFakeOutbox([]sap.OutboxMessage{
		sap.NewOutboxMessageForTesting(1, recordURI, json.RawMessage("null"),
			func(ctx context.Context) error { return nil },
		),
	})

	indexer := NewIndexer(index, outbox)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = indexer.Run(ctx)

	require.Equal(t, []string{recordURI.String()}, index.deleted)
}

func TestIndexer_DoesNotAckOnHandleFailure(t *testing.T) {
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:org1/network.habitat.space/skey1/did:plc:user1/network.habitat.note/rkey1",
	)
	index := &fakeIndex{}
	var acked atomic.Bool
	// Malformed JSON: handleMessage fails to unmarshal it, so it must be
	// left unacked for redelivery on the next Poll.
	outbox := newFakeOutbox([]sap.OutboxMessage{
		sap.NewOutboxMessageForTesting(1, recordURI, json.RawMessage("not-json"),
			func(ctx context.Context) error {
				acked.Store(true)
				return nil
			},
		),
	})

	indexer := NewIndexer(index, outbox)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_ = indexer.Run(ctx)

	require.Empty(t, index.upserted)
	require.False(t, acked.Load(), "message should not be acked when indexing fails")
}
