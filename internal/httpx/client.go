package httpx

import (
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/utils"
	"github.com/hashicorp/go-retryablehttp"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// NewClient returns an *http.Client instrumented with OpenTelemetry: each
// outbound request gets a span and client metrics recorded against the global
// tracer/meter providers (see internal/telemetry). It wraps http.DefaultTransport,
// so every client returned shares the process-wide connection pool.
func NewClient(opts ...utils.Opt[http.Client]) *http.Client {
	c := utils.ResolveOptions(
		http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)},
		opts,
	)
	return &c
}

// WithRetry wraps the client's transport with go-retryablehttp's
// RoundTripper, retrying transient failures (network errors, 5xx, 429) with
// exponential backoff. Useful for calls that can race another service's own
// startup, e.g. a client hitting a dependency before its listener is up.
func WithRetry() utils.Opt[http.Client] {
	return func(c *http.Client) {
		retryClient := retryablehttp.NewClient()
		retryClient.HTTPClient = &http.Client{Transport: c.Transport}
		retryClient.Logger = retryLogger{}
		*c = *retryClient.StandardClient()
	}
}

// retryLogger adapts go-retryablehttp's LeveledLogger interface to slog,
// instead of retryablehttp's default of an unstructured *log.Logger to stderr.
type retryLogger struct{}

func (retryLogger) Error(msg string, kv ...any) { slog.Error(msg, kv...) }
func (retryLogger) Info(msg string, kv ...any)  { slog.Info(msg, kv...) }
func (retryLogger) Debug(msg string, kv ...any) { slog.Debug(msg, kv...) }
func (retryLogger) Warn(msg string, kv ...any)  { slog.Warn(msg, kv...) }
