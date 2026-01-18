package telemetry

import (
	"context"
	"os"

	zerolog "github.com/rs/zerolog"
	"go.opentelemetry.io/contrib/bridges/otelzerolog"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"

	otellog "go.opentelemetry.io/otel/sdk/log"
)

// Expected usage is that every package should have a one-line file that does:
// var log = logging.InstrumentedLogger()
// Now, using log.* anywhere will use the logger that sends logs via OpenTelemetry to Grafana.

// Copied from: https://pkg.go.dev/go.opentelemetry.io/contrib/bridges/otelzerolog

func InstrumentedLogger() zerolog.Logger {
	exporter, err := otlploggrpc.New(context.Background())
	if err != nil {
		panic(err)
	}

	lp := otellog.NewLoggerProvider(
		otellog.WithProcessor(otellog.NewBatchProcessor(exporter)),
		otellog.WithResource(habitatResourceDefinition()),
	)

	logger := zerolog.New(
		zerolog.MultiLevelWriter(
			os.Stdout,
			otelzerolog.NewWriter("my-service"),
		),
	)
	// Create a logger that emits logs to both STDOUT and the OTel Go SDK.
	hook := otelzerolog.NewHook("habitat", otelzerolog.WithLoggerProvider(lp))
	logger := zerolog.New(os.Stdout).With().Logger()
	logger = logger.Hook(hook)
	return logger
}
