package search

import (
	"context"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/telemetry"
)

// recordSpans installs a global tracer provider that records every span, and
// returns its recorder. Package tracers bind to the first global provider, so
// it is installed once per test binary.
var recordSpans = sync.OnceValue(func() *tracetest.SpanRecorder {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	return rec
})

// endedSpans returns the ended spans named name for which match holds.
func endedSpans(
	rec *tracetest.SpanRecorder,
	name string,
	match func(sdktrace.ReadOnlySpan) bool,
) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == name && match(s) {
			out = append(out, s)
		}
	}
	return out
}

// aboutRecord matches spans whose record URI attribute is uri.
func aboutRecord(uri habitat_syntax.SpaceRecordURI) func(sdktrace.ReadOnlySpan) bool {
	return func(s sdktrace.ReadOnlySpan) bool {
		for _, a := range s.Attributes() {
			if a.Key == telemetry.RecordURIKey && a.Value.AsString() == uri.String() {
				return true
			}
		}
		return false
	}
}

func putIn(
	ctx context.Context,
	t *testing.T,
	it *indexerTest,
	space habitat_syntax.SpaceURI,
	rkey string,
) {
	t.Helper()
	_, _, err := it.store.PutRecord(ctx, space, testRepo, testColl, syntax.RecordKey(rkey),
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "note " + rkey}))
	require.NoError(t, err)
}

func TestIndexerTracesWritesToTheirIndexing(t *testing.T) {
	rec := recordSpans()
	it := setupIndexer(t)
	testTracer := otel.Tracer("test")
	space, err := it.store.CreateSpace(t.Context(), testOrg, testType, "traced")
	require.NoError(t, err)

	// A job queued by one write runs in a trace of its own, linked to the
	// write, since indexing happens after the request has been answered.
	ctx, request := testTracer.Start(t.Context(), "request")
	putIn(ctx, t, it, space, "k1")
	request.End()
	it.drain(t.Context())

	uri := recordURI(space, "k1")
	writes := endedSpans(rec, "spaces.PutRecord", aboutRecord(uri))
	require.Len(t, writes, 1)
	write := writes[0].SpanContext()
	jobs := endedSpans(rec, "search.indexer index_repo", func(s sdktrace.ReadOnlySpan) bool {
		for _, l := range s.Links() {
			if l.SpanContext.SpanID() == write.SpanID() {
				return true
			}
		}
		return false
	})
	require.Len(t, jobs, 1)
	job := jobs[0]
	require.False(t, job.Parent().IsValid())
	require.NotEqual(t, write.TraceID(), job.SpanContext().TraceID())
	require.Equal(t, trace.SpanKindConsumer, job.SpanKind())
	docs := endedSpans(rec, "search.indexer document", aboutRecord(uri))
	require.Len(t, docs, 1)
	require.Equal(t, job.SpanContext().SpanID(), docs[0].Parent().SpanID())

	// Writes collapsed into one job are indexed in a trace of their own,
	// linked to each of them.
	var linked []trace.SpanContext
	for _, rkey := range []string{"k2", "k3"} {
		ctx, request := testTracer.Start(t.Context(), "request "+rkey)
		putIn(ctx, t, it, space, rkey)
		request.End()
		writes := endedSpans(rec, "spaces.PutRecord", aboutRecord(recordURI(space, rkey)))
		require.Len(t, writes, 1)
		linked = append(linked, writes[0].SpanContext())
	}
	it.drain(t.Context())
	jobs = endedSpans(rec, "search.indexer index_repo", func(s sdktrace.ReadOnlySpan) bool {
		return len(s.Links()) == 2
	})
	require.Len(t, jobs, 1)
	job = jobs[0]
	require.False(t, job.Parent().IsValid())
	for i, l := range job.Links() {
		require.Equal(t, linked[i], l.SpanContext)
		require.NotEqual(t, linked[i].TraceID(), job.SpanContext().TraceID())
	}
	for _, rkey := range []string{"k2", "k3"} {
		docs := endedSpans(rec, "search.indexer document", aboutRecord(recordURI(space, rkey)))
		require.Len(t, docs, 1)
		require.Equal(t, job.SpanContext().TraceID(), docs[0].SpanContext().TraceID())
	}
}
