package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/events"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openIndexerTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "test.db")), &gorm.Config{})
	require.NoError(t, err)
	return db
}

// fakePearClient is a test double for PearClient: SubscribeSpaces replays a
// fixed list of events synchronously (no real HTTP/SSE connection), and
// ResolveCallerOrg returns a canned org DID or error. Shared by indexer and
// server tests so there's a single mock to maintain.
type fakePearClient struct {
	events     []events.Event
	orgDID     syntax.DID
	resolveErr error
}

var _ PearClient = (*fakePearClient)(nil)

func (f *fakePearClient) SubscribeSpaces(
	ctx context.Context,
	cursor uint64,
	onEvent func(events.Event),
) error {
	for _, e := range f.events {
		if e.Seq <= cursor {
			continue
		}
		onEvent(e)
	}
	<-ctx.Done()
	return ctx.Err()
}

func (f *fakePearClient) ResolveCallerOrg(
	ctx context.Context,
	bearerToken string,
) (syntax.DID, error) {
	if f.resolveErr != nil {
		return "", f.resolveErr
	}
	return f.orgDID, nil
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

func TestIndexer_UpsertsCreateAndUpdateEvents(t *testing.T) {
	db := openIndexerTestDB(t)
	index := &fakeIndex{}
	spaceURI := habitat_syntax.SpaceURI("ats://did:plc:org1/app.space/skey1")
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:org1/app.space/skey1/did:plc:user1/network.habitat.note/rkey1",
	)
	source := &fakePearClient{events: []events.Event{
		{
			Seq:   1,
			Type:  "space",
			Space: spaceURI,
			Ops: []events.EventOps{
				{Action: "create", Uri: recordURI, Value: map[string]any{"title": "Budget"}},
			},
		},
	}}

	indexer, err := NewIndexer(db, index, source)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = indexer.Run(ctx)

	require.Len(t, index.upserted, 1)
	require.Equal(t, recordURI, index.upserted[0].URI)
	require.Equal(t, syntax.DID("did:plc:org1"), index.upserted[0].OrgDID)
	require.Equal(t, syntax.NSID("network.habitat.note"), index.upserted[0].Collection)
	require.Contains(t, index.upserted[0].Content, "Budget")
}

func TestIndexer_DeletesOnDeleteAction(t *testing.T) {
	db := openIndexerTestDB(t)
	index := &fakeIndex{}
	spaceURI := habitat_syntax.SpaceURI("ats://did:plc:org1/app.space/skey1")
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:org1/app.space/skey1/did:plc:user1/network.habitat.note/rkey1",
	)
	source := &fakePearClient{events: []events.Event{
		{
			Seq:   1,
			Type:  "space",
			Space: spaceURI,
			Ops:   []events.EventOps{{Action: "delete", Uri: recordURI}},
		},
	}}

	indexer, err := NewIndexer(db, index, source)
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = indexer.Run(ctx)

	require.Equal(t, []string{recordURI.String()}, index.deleted)
}

func TestIndexer_PersistsCursorAcrossRuns(t *testing.T) {
	db := openIndexerTestDB(t)
	index := &fakeIndex{}
	spaceURI := habitat_syntax.SpaceURI("ats://did:plc:org1/app.space/skey1")
	recordURI := habitat_syntax.SpaceRecordURI(
		"ats://did:plc:org1/app.space/skey1/did:plc:user1/network.habitat.note/rkey1",
	)
	allEvents := []events.Event{
		{Seq: 1, Type: "space", Space: spaceURI, Ops: []events.EventOps{
			{Action: "create", Uri: recordURI, Value: map[string]any{"title": "first"}},
		}},
	}

	indexer, err := NewIndexer(db, index, &fakePearClient{events: allEvents})
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	_ = indexer.Run(ctx)
	cancel()
	require.Len(t, index.upserted, 1)

	// A second run with the same already-seen event should not re-index it,
	// since the persisted cursor is now >= 1.
	indexer2, err := NewIndexer(db, index, &fakePearClient{events: allEvents})
	require.NoError(t, err)
	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	_ = indexer2.Run(ctx2)
	cancel2()
	require.Len(
		t,
		index.upserted,
		1,
		"event already at or before the persisted cursor should not be re-indexed",
	)
}
