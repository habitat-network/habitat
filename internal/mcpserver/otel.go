package mcpserver

import (
	"context"
	"errors"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// OpenTelemetry instrumentation for the MCP server, following the GenAI MCP
// semantic conventions:
// https://github.com/open-telemetry/semantic-conventions/blob/e126ea9105b15912ccd80deab98929025189b696/docs/gen-ai/mcp.md

const otelScope = "github.com/habitat-network/habitat/internal/mcpserver"

// durationBuckets are the explicit bucket boundaries (seconds) the conventions
// recommend for the MCP duration histograms.
var durationBuckets = []float64{0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1, 2, 5, 10, 30, 60, 120, 300}

// otelMiddlewareConfig overrides the global OpenTelemetry providers; tests use
// it to capture telemetry in memory.
type otelMiddlewareConfig struct {
	tracerProvider trace.TracerProvider
	meterProvider  metric.MeterProvider
}

// metaCarrier adapts a request's params._meta property bag to a
// propagation.TextMapCarrier so trace context can be extracted from it.
type metaCarrier map[string]any

func (c metaCarrier) Get(key string) string {
	v, _ := c[key].(string)
	return v
}

func (c metaCarrier) Set(key, value string) { c[key] = value }

func (c metaCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// otelMiddleware returns receiving middleware that traces each incoming MCP
// request or notification and records the mcp.server.* metrics.
//
// Trace context (W3C traceparent/tracestate and baggage) is extracted from the
// request's params._meta and used as the parent of the server span. If the
// transport (e.g. otelhttp) already put a span in ctx, it is attached as a
// link, since the MCP and HTTP contexts are independent.
func otelMiddleware(cfg otelMiddlewareConfig) mcp.Middleware {
	if cfg.tracerProvider == nil {
		cfg.tracerProvider = otel.GetTracerProvider()
	}
	if cfg.meterProvider == nil {
		cfg.meterProvider = otel.GetMeterProvider()
	}
	tracer := cfg.tracerProvider.Tracer(otelScope)
	meter := cfg.meterProvider.Meter(otelScope)
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

	// Instrument creation only fails for invalid names/options; the no-op
	// fallbacks returned alongside the error are safe to use.
	opDuration, _ := meter.Float64Histogram(
		"mcp.server.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("MCP request or notification duration as observed on the receiver from the time it was received until the result or ack is sent."),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)
	sessionDuration, _ := meter.Float64Histogram(
		"mcp.server.session.duration",
		metric.WithUnit("s"),
		metric.WithDescription("The duration of the MCP session as observed on the MCP server."),
		metric.WithExplicitBucketBoundaries(durationBuckets...),
	)

	// sessions holds the sessions whose duration is already being timed.
	var sessions sync.Map

	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			start := time.Now()
			params := req.GetParams()

			// Parent the span on the context carried in params._meta, linking
			// any ambient (transport) span context.
			var linkOpts []trace.SpanStartOption
			if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
				linkOpts = append(linkOpts, trace.WithLinks(trace.Link{SpanContext: sc}))
			}
			if meta := paramsMeta(params); meta != nil {
				ctx = prop.Extract(ctx, metaCarrier(meta))
			}

			target, targetAttrs := otelTarget(method, params)
			spanName := method
			if target != "" {
				spanName = method + " " + target
			}
			attrs := append([]attribute.KeyValue{attribute.String("mcp.method.name", method)}, targetAttrs...)
			attrs = append(attrs, sessionAttrs(req)...)

			ctx, span := tracer.Start(ctx, spanName,
				append(linkOpts, trace.WithSpanKind(trace.SpanKindServer), trace.WithAttributes(attrs...))...)
			defer span.End()

			result, err := next(ctx, method, req)

			var errType string
			var errMsg string
			var rpcErr *jsonrpc.Error
			switch {
			case errors.As(err, &rpcErr):
				errType = strconv.FormatInt(rpcErr.Code, 10)
				errMsg = rpcErr.Message
				span.SetAttributes(attribute.String("rpc.response.status_code", errType))
			case err != nil:
				errType = "_OTHER"
				errMsg = err.Error()
			default:
				// A tool failure is a successful JSON-RPC call whose result
				// has isError set.
				if res, ok := result.(*mcp.CallToolResult); ok && res != nil && res.IsError {
					errType = "tool_error"
				}
			}
			if errType != "" {
				attrs = append(attrs, attribute.String("error.type", errType))
				span.SetAttributes(attribute.String("error.type", errType))
				span.SetStatus(codes.Error, errMsg)
			}

			opDuration.Record(ctx, time.Since(start).Seconds(),
				metric.WithAttributes(metricAttrs(attrs)...))

			// The SDK offers no session-closed hook, and initialize and
			// notifications/initialized bypass receiving middleware, so time
			// the session from the first request seen on it until it closes.
			if ss, ok := req.GetSession().(*mcp.ServerSession); ok && ss != nil {
				if _, seen := sessions.LoadOrStore(ss, struct{}{}); !seen {
					go func() {
						_ = ss.Wait()
						sessions.Delete(ss)
						sessionDuration.Record(context.WithoutCancel(ctx), time.Since(start).Seconds())
					}()
				}
			}
			return result, err
		}
	}
}

// paramsMeta returns params._meta, or nil. Requests without params carry a
// typed nil pointer in the Params interface, which isn't == nil and whose
// GetMeta would panic.
func paramsMeta(params mcp.Params) map[string]any {
	if v := reflect.ValueOf(params); !v.IsValid() || (v.Kind() == reflect.Pointer && v.IsNil()) {
		return nil
	}
	return params.GetMeta()
}

// otelTarget returns the low-cardinality span name target for a request (the
// tool or prompt name) along with the attributes that identify it. Resource
// URIs are high cardinality, so they're an attribute but never part of the
// span name.
func otelTarget(method string, params mcp.Params) (string, []attribute.KeyValue) {
	switch p := params.(type) {
	case *mcp.CallToolParamsRaw:
		return p.Name, []attribute.KeyValue{
			attribute.String("gen_ai.tool.name", p.Name),
			attribute.String("gen_ai.operation.name", "execute_tool"),
		}
	case *mcp.CallToolParams:
		return p.Name, []attribute.KeyValue{
			attribute.String("gen_ai.tool.name", p.Name),
			attribute.String("gen_ai.operation.name", "execute_tool"),
		}
	case *mcp.GetPromptParams:
		return p.Name, []attribute.KeyValue{attribute.String("gen_ai.prompt.name", p.Name)}
	case *mcp.ReadResourceParams:
		return "", []attribute.KeyValue{attribute.String("mcp.resource.uri", p.URI)}
	}
	return "", nil
}

// sessionAttrs returns the session and protocol attributes of a request.
func sessionAttrs(req mcp.Request) []attribute.KeyValue {
	var attrs []attribute.KeyValue
	ss, ok := req.GetSession().(*mcp.ServerSession)
	if !ok || ss == nil {
		return attrs
	}
	if id := ss.ID(); id != "" {
		attrs = append(attrs, attribute.String("mcp.session.id", id))
	}
	if init := ss.InitializeParams(); init != nil && init.ProtocolVersion != "" {
		attrs = append(attrs, attribute.String("mcp.protocol.version", init.ProtocolVersion))
	}
	if extra := req.GetExtra(); extra != nil && extra.Header != nil {
		attrs = append(attrs,
			attribute.String("network.protocol.name", "http"),
			attribute.String("network.transport", "tcp"))
	} else {
		attrs = append(attrs, attribute.String("network.transport", "pipe"))
	}
	return attrs
}

// metricAttrs filters span attributes down to those the conventions list for
// mcp.server.operation.duration, dropping high-cardinality ones such as the
// session id and resource URI (opt-in).
func metricAttrs(attrs []attribute.KeyValue) []attribute.KeyValue {
	var out []attribute.KeyValue
	for _, a := range attrs {
		switch a.Key {
		case "mcp.session.id", "mcp.resource.uri":
		default:
			out = append(out, a)
		}
	}
	return out
}
