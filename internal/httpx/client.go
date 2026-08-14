package httpx

import (
	"net/http"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewClient returns an *http.Client instrumented with OpenTelemetry: each
// outbound request gets a span and client metrics recorded against the global
// tracer/meter providers (see internal/telemetry). It wraps http.DefaultTransport,
// so every client returned shares the process-wide connection pool.
func NewClient() *http.Client {
	return &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
}
