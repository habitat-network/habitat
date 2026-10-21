package log

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

func TestLog(t *testing.T) {
	New(WithLevel("debug"), WithStdout(true))
}

func TestTraceHandlerAddsSpanIDs(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(traceHandler{slog.NewTextHandler(&buf, nil)}).With("k", "v")

	logger.InfoContext(context.Background(), "untraced")
	require.NotContains(t, buf.String(), "trace_id")

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
	})
	buf.Reset()
	ctx := trace.ContextWithSpanContext(context.Background(), sc)
	logger.WithGroup("g").InfoContext(ctx, "traced")
	require.Contains(t, buf.String(), "trace_id="+sc.TraceID().String())
	require.Contains(t, buf.String(), "span_id="+sc.SpanID().String())
}
