package telemetry

import (
	"context"
	"io"

	"go.opentelemetry.io/otel/log"
)

// We need to set up a custom io.Writer to get zerolog to go to an OpenTelemetry collector.

type otelWriter struct {
	logger log.Logger
}

func (w *otelWriter) Write(p []byte) (int, error) {
	rec := log.Record{}
	rec.SetBody(log.StringValue(string(p))) // unavoidable copy

	w.logger.Emit(context.Background(), rec)
	return len(p), nil
}

func NewOtelLogWriter(logger log.Logger) io.Writer {
	return &otelWriter{
		logger: logger,
	}
}
