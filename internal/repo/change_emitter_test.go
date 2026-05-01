package repo

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
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
