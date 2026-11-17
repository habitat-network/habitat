package mcpserver

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

const testTraceparent = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

type otelHarness struct {
	session *mcp.ClientSession
	spans   *tracetest.SpanRecorder
	metrics *sdkmetric.ManualReader
}

// newOtelHarness connects a client to a server with the OTel middleware and
// two tools: "ok" and "fail" (which returns an isError result).
func newOtelHarness(t *testing.T) *otelHarness {
	t.Helper()
	spans := tracetest.NewSpanRecorder()
	reader := sdkmetric.NewManualReader()
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	srv.AddReceivingMiddleware(otelMiddleware(otelMiddlewareConfig{
		tracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)),
		meterProvider:  sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)),
	}))
	type in struct{}
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: "ok"},
		func(context.Context, *mcp.CallToolRequest, in) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				Content: []mcp.Content{&mcp.TextContent{Text: "hi"}},
			}, nil, nil
		},
	)
	mcp.AddTool(
		srv,
		&mcp.Tool{Name: "fail"},
		func(context.Context, *mcp.CallToolRequest, in) (*mcp.CallToolResult, any, error) {
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: "no"}},
			}, nil, nil
		},
	)

	ct, st := mcp.NewInMemoryTransports()
	ctx := t.Context()
	ss, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).
		Connect(ctx, ct, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = cs.Close() })
	return &otelHarness{session: cs, spans: spans, metrics: reader}
}

func (h *otelHarness) span(t *testing.T, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range h.spans.Ended() {
		if s.Name() == name {
			return s
		}
	}
	require.FailNowf(t, "span not found", "no span named %q", name)
	return nil
}

func attrMap(kvs []attribute.KeyValue) map[string]string {
	m := map[string]string{}
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.String()
	}
	return m
}

func (h *otelHarness) histogram(t *testing.T, name string) metricdata.Histogram[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.metrics.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m.Data.(metricdata.Histogram[float64])
			}
		}
	}
	require.FailNowf(t, "metric not found", "no metric named %q", name)
	return metricdata.Histogram[float64]{}
}

func TestOtelMiddleware_ToolCallSpanParentedByMeta(t *testing.T) {
	h := newOtelHarness(t)
	_, err := h.session.CallTool(t.Context(), &mcp.CallToolParams{
		Meta:      mcp.Meta{"traceparent": testTraceparent},
		Name:      "ok",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)

	s := h.span(t, "tools/call ok")
	require.Equal(t, trace.SpanKindServer, s.SpanKind())
	// The span continues the trace carried in params._meta.
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", s.SpanContext().TraceID().String())
	require.Equal(t, "00f067aa0ba902b7", s.Parent().SpanID().String())
	attrs := attrMap(s.Attributes())
	require.Equal(t, "tools/call", attrs["mcp.method.name"])
	require.Equal(t, "ok", attrs["gen_ai.tool.name"])
	require.Equal(t, "execute_tool", attrs["gen_ai.operation.name"])
	require.Equal(t, "pipe", attrs["network.transport"])
	require.NotContains(t, attrs, "error.type")
	require.Equal(t, codes.Unset, s.Status().Code)
}

func TestOtelMiddleware_LinksAmbientContext(t *testing.T) {
	h := newOtelHarness(t)
	parent := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    trace.TraceID{1},
		SpanID:     trace.SpanID{2},
		TraceFlags: trace.FlagsSampled,
	})
	ctx := trace.ContextWithSpanContext(t.Context(), parent)
	// In-memory transport doesn't carry ctx to the server, so drive the
	// middleware directly with an ambient span context.
	handler := otelMiddleware(otelMiddlewareConfig{
		tracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(h.spans)),
	})(func(context.Context, string, mcp.Request) (mcp.Result, error) { return nil, nil })
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: &mcp.CallToolParamsRaw{
		Meta: mcp.Meta{"traceparent": testTraceparent}, Name: "ok",
	}}
	_, err := handler(ctx, "tools/call", req)
	require.NoError(t, err)

	var found sdktrace.ReadOnlySpan
	for _, s := range h.spans.Ended() {
		if len(s.Links()) == 1 {
			found = s
		}
	}
	require.NotNil(t, found)
	require.Equal(t, parent.TraceID(), found.Links()[0].SpanContext.TraceID())
	require.Equal(t, "4bf92f3577b34da6a3ce929d0e0e4736", found.SpanContext().TraceID().String())
}

func TestOtelMiddleware_ToolErrorMarksSpan(t *testing.T) {
	h := newOtelHarness(t)
	_, err := h.session.CallTool(
		t.Context(),
		&mcp.CallToolParams{Name: "fail", Arguments: map[string]any{}},
	)
	require.NoError(t, err)

	s := h.span(t, "tools/call fail")
	require.Equal(t, "tool_error", attrMap(s.Attributes())["error.type"])
	require.Equal(t, codes.Error, s.Status().Code)
}

func TestOtelMiddleware_JSONRPCErrorUsesCode(t *testing.T) {
	h := newOtelHarness(t)
	// Unknown tool names yield a JSON-RPC protocol error.
	_, err := h.session.CallTool(
		t.Context(),
		&mcp.CallToolParams{Name: "missing", Arguments: map[string]any{}},
	)
	require.Error(t, err)

	s := h.span(t, "tools/call missing")
	attrs := attrMap(s.Attributes())
	require.Regexp(t, `^-?\d+$`, attrs["error.type"])
	require.Equal(t, attrs["error.type"], attrs["rpc.response.status_code"])
	require.Equal(t, codes.Error, s.Status().Code)
}

func TestOtelMiddleware_MethodWithoutTargetUsesMethodAsName(t *testing.T) {
	h := newOtelHarness(t)
	_, err := h.session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	s := h.span(t, "tools/list")
	require.NotContains(t, attrMap(s.Attributes()), "gen_ai.tool.name")
}

func TestOtelMiddleware_RecordsOperationDuration(t *testing.T) {
	h := newOtelHarness(t)
	_, err := h.session.CallTool(
		t.Context(),
		&mcp.CallToolParams{Name: "ok", Arguments: map[string]any{}},
	)
	require.NoError(t, err)
	_, err = h.session.CallTool(
		t.Context(),
		&mcp.CallToolParams{Name: "fail", Arguments: map[string]any{}},
	)
	require.NoError(t, err)

	hist := h.histogram(t, "mcp.server.operation.duration")
	var okPoint, failPoint *metricdata.HistogramDataPoint[float64]
	for i, dp := range hist.DataPoints {
		attrs := attrMap(dp.Attributes.ToSlice())
		require.NotContains(
			t,
			attrs,
			"mcp.session.id",
			"session id is too high-cardinality for a metric",
		)
		if attrs["mcp.method.name"] == "tools/call" && attrs["gen_ai.tool.name"] == "ok" {
			okPoint = &hist.DataPoints[i]
		}
		if attrs["gen_ai.tool.name"] == "fail" {
			failPoint = &hist.DataPoints[i]
			require.Equal(t, "tool_error", attrs["error.type"])
		}
	}
	require.NotNil(t, okPoint)
	require.NotNil(t, failPoint)
	require.Equal(t, uint64(1), okPoint.Count)
	require.Equal(
		t,
		[]float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, 300},
		okPoint.Bounds,
	)
}

func TestOtelMiddleware_RecordsSessionDurationOnClose(t *testing.T) {
	h := newOtelHarness(t)
	// Round-trip a request so the initialized notification has been handled.
	_, err := h.session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.NoError(t, h.session.Close())

	require.Eventually(t, func() bool {
		var rm metricdata.ResourceMetrics
		if err := h.metrics.Collect(context.Background(), &rm); err != nil {
			return false
		}
		for _, sm := range rm.ScopeMetrics {
			for _, m := range sm.Metrics {
				if m.Name == "mcp.server.session.duration" {
					return len(m.Data.(metricdata.Histogram[float64]).DataPoints) > 0
				}
			}
		}
		return false
	}, 5e9, 10e6)
}

func TestMetaCarrier(t *testing.T) {
	c := metaCarrier{"traceparent": "x", "n": 1}
	require.Equal(t, "x", c.Get("traceparent"))
	require.Equal(t, "", c.Get("n"), "non-string values are ignored")
	c.Set("a", "b")
	require.ElementsMatch(t, []string{"traceparent", "n", "a"}, c.Keys())
}
