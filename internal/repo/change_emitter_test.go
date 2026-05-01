package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestConsumeOnce(t *testing.T) {
	ctx := context.Background()
	ce := newChangeEmitter(ctx, 10)

	s, err := ce.Consume()
	require.NoError(t, err)
	require.NotNil(t, s)
}

func TestConsumeTwiceErrors(t *testing.T) {
	ctx := context.Background()
	ce := newChangeEmitter(ctx, 10)

	_, err := ce.Consume()
	require.NoError(t, err)

	_, err = ce.Consume()
	require.Error(t, err)
}

func TestEmitBeforeConsumeIsNoop(t *testing.T) {
	ctx := context.Background()
	ce := newChangeEmitter(ctx, 10)

	// Should not block or panic with no consumer attached.
	ce.EmitChangeEvent("did:example:123", "app.test.record", "rkey1", OperationCreate, time.Now(), json.RawMessage(`{}`))
}

func TestEmitAfterConsumeDeliversEvent(t *testing.T) {
	ctx := context.Background()
	ce := newChangeEmitter(ctx, 10)

	s, err := ce.Consume()
	require.NoError(t, err)

	did := "did:example:abc"
	collection := "app.test.record"
	rkey := "rkey1"
	record := json.RawMessage(`{"foo":"bar"}`)
	ts := time.Now().Truncate(time.Nanosecond)

	ce.EmitChangeEvent(did, collection, rkey, OperationCreate, ts, record)

	ev, err := s.Next(ctx)
	require.NoError(t, err)
	require.Equal(t, did, ev.Did)
	require.Equal(t, ts.UnixNano(), ev.TimeUS)
	require.NotNil(t, ev.Commit)
	require.Equal(t, string(OperationCreate), ev.Commit.Operation)
	require.Equal(t, collection, ev.Commit.Collection)
	require.Equal(t, rkey, ev.Commit.Rev)
	require.Equal(t, record, ev.Commit.Record)
}

func newTestRepo(t *testing.T) *repo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	r, err := NewRepo(t.Context(), db)
	require.NoError(t, err)
	return r
}

func TestPutRecordEmitsCreate(t *testing.T) {
	r := newTestRepo(t)
	s, err := r.ce.Consume()
	require.NoError(t, err)

	_, err = r.PutRecord(t.Context(), Record{
		Did:        "did:plc:test",
		Collection: "network.habitat.test",
		Rkey:       "rkey-1",
		Value:      map[string]any{"msg": "hello"},
	}, nil)
	require.NoError(t, err)

	ev, err := s.Next(t.Context())
	require.NoError(t, err)
	require.Equal(t, string(OperationCreate), ev.Commit.Operation)
	require.Equal(t, "did:plc:test", ev.Did)
	require.Equal(t, "network.habitat.test", ev.Commit.Collection)
	require.Equal(t, "rkey-1", ev.Commit.Rev)
	require.NotNil(t, ev.Commit.Record)
}

func TestPutRecordEmitsUpdate(t *testing.T) {
	r := newTestRepo(t)
	s, err := r.ce.Consume()
	require.NoError(t, err)

	rec := Record{Did: "did:plc:test", Collection: "network.habitat.test", Rkey: "rkey-1", Value: map[string]any{"msg": "hello"}}

	_, err = r.PutRecord(t.Context(), rec, nil)
	require.NoError(t, err)
	// Drain the create event.
	_, err = s.Next(t.Context())
	require.NoError(t, err)

	rec.Value = map[string]any{"msg": "world"}
	_, err = r.PutRecord(t.Context(), rec, nil)
	require.NoError(t, err)

	ev, err := s.Next(t.Context())
	require.NoError(t, err)
	require.Equal(t, string(OperationUpdate), ev.Commit.Operation)
}

func TestCreateRecordEmitsCreate(t *testing.T) {
	r := newTestRepo(t)
	s, err := r.ce.Consume()
	require.NoError(t, err)

	_, err = r.CreateRecord(t.Context(), Record{
		Did:        "did:plc:test",
		Collection: "network.habitat.test",
		Rkey:       "rkey-1",
		Value:      map[string]any{"msg": "hello"},
	}, nil)
	require.NoError(t, err)

	ev, err := s.Next(t.Context())
	require.NoError(t, err)
	require.Equal(t, string(OperationCreate), ev.Commit.Operation)
	require.Equal(t, "did:plc:test", ev.Did)
	require.Equal(t, "network.habitat.test", ev.Commit.Collection)
	require.Equal(t, "rkey-1", ev.Commit.Rev)
	require.NotNil(t, ev.Commit.Record)
}

func TestDeleteRecordEmitsDelete(t *testing.T) {
	r := newTestRepo(t)
	s, err := r.ce.Consume()
	require.NoError(t, err)

	_, err = r.PutRecord(t.Context(), Record{
		Did:        "did:plc:test",
		Collection: "network.habitat.test",
		Rkey:       "rkey-1",
		Value:      map[string]any{"msg": "hello"},
	}, nil)
	require.NoError(t, err)
	// Drain the create event.
	_, err = s.Next(t.Context())
	require.NoError(t, err)

	err = r.DeleteRecord(t.Context(), "did:plc:test", "network.habitat.test", "rkey-1")
	require.NoError(t, err)

	ev, err := s.Next(t.Context())
	require.NoError(t, err)
	require.Equal(t, string(OperationDelete), ev.Commit.Operation)
	require.Nil(t, ev.Commit.Record)
}
