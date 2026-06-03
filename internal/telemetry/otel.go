package telemetry

// OpenTelemetry integration
import (
	"context"
	"log/slog"
	"os"

	"errors"

	"time"

	"github.com/habitat-network/habitat/internal/utils"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"

	"go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/global"
	lognoop "go.opentelemetry.io/otel/log/noop"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func habitatResourceDefinition() (*resource.Resource, error) {
	env := utils.GetEnvString("env", "unknown")
	resource, err := resource.New(
		context.Background(),
		resource.WithFromEnv(),
		resource.WithOSDescription(),
		resource.WithAttributes(
			semconv.ServiceNameKey.String("habitat"),
			attribute.String("env", env),
		),
	)
	if err != nil {
		return nil, err
	}
	return resource, nil
}

func setupTraceProvider(
	ctx context.Context,
	resource *resource.Resource,
	shutdownFuncs *[]func(context.Context) error,
) (trace.TracerProvider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" &&
		os.Getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT") == "" {
		slog.Info("using no-op tracer provider")
		return tracenoop.NewTracerProvider(), nil
	}
	exporter, err := otlptrace.New(ctx, otlptracehttp.NewClient())
	if err != nil {
		return nil, err
	}
	provider := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exporter),
		tracesdk.WithResource(resource),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
	)
	*shutdownFuncs = append(*shutdownFuncs, func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	})
	return provider, nil
}

func setupMetricsProvider(
	ctx context.Context,
	resource *resource.Resource,
	shutdownFuncs *[]func(context.Context) error,
) (metric.MeterProvider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" &&
		os.Getenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT") == "" {
		slog.Info("using no-op meter provider")
		return metricnoop.NewMeterProvider(), nil
	}
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	provider := metricsdk.NewMeterProvider(
		metricsdk.WithReader(metricsdk.NewPeriodicReader(exporter)),
		metricsdk.WithResource(resource),
	)
	*shutdownFuncs = append(*shutdownFuncs, func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	})
	return provider, nil
}

func setupLoggingProvider(
	ctx context.Context,
	resource *resource.Resource,
	shutdownFuncs *[]func(context.Context) error,
) (log.LoggerProvider, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" &&
		os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT") == "" {
		slog.Info("using no-op logger provider")
		return lognoop.NewLoggerProvider(), nil
	}
	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	)
	if err != nil {
		return nil, err
	}
	provider := logsdk.NewLoggerProvider(
		logsdk.WithProcessor(logsdk.NewBatchProcessor(exporter)),
		logsdk.WithResource(resource),
	)
	*shutdownFuncs = append(*shutdownFuncs, func(ctx context.Context) error {
		return provider.Shutdown(ctx)
	})
	return provider, nil
}

// SetupOpenTelemetry bootstraps the OpenTelemetry pipeline for metrics, tracing, and logginc.
// If no OTLP endpoint is configured (via OTEL_EXPORTER_OTLP_ENDPOINT or per-signal env vars),
// it returns a no-op shutdown function with no error, avoiding background connection errors
// when no OTel collector is running.
// If it does not return an error, make sure to call shutdown for proper cleanup at process exit.
// The logging provider needs to be hooked into the logger of your choice (e.g. slog) for logs to show
// in OTel / Grafana.
//
// This code was more or less copied from the golang OpenTelemetry tutorial.
func SetupOpenTelemetry(ctx context.Context) (func(context.Context) error, error) {
	var shutdownFuncs []func(context.Context) error
	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	// handleErr calls shutdown for cleanup and makes sure that the entire chain of errors is returned
	var err error
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	resource, err := habitatResourceDefinition()
	if err != nil {
		handleErr(err)
		return shutdown, err
	}

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(prop)

	traceProvider, err := setupTraceProvider(ctx, resource, &shutdownFuncs)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	otel.SetTracerProvider(traceProvider)

	meterProvider, err := setupMetricsProvider(ctx, resource, &shutdownFuncs)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	otel.SetMeterProvider(meterProvider)

	loggerProvider, err := setupLoggingProvider(ctx, resource, &shutdownFuncs)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	global.SetLoggerProvider(loggerProvider)

	err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		handleErr(err)
	}
	return shutdown, err
}
