package telemetry

// OpenTelemetry integration
import (
	"context"
	"errors"
	"time"

	"github.com/eagraf/habitat-new/internal/utils"

	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	otellog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
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

func setupTraceProvider(ctx context.Context, resource *resource.Resource) (*trace.TracerProvider, error) {
	exporter, err := otlptrace.New(ctx, otlptracehttp.NewClient())
	if err != nil {
		return nil, err
	}

	provider := trace.NewTracerProvider(trace.WithBatcher(exporter), trace.WithResource(resource))
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func setupMetricsProvider(ctx context.Context, resource *resource.Resource) (*metric.MeterProvider, error) {
	exporter, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}

	provider := metric.NewMeterProvider(metric.WithReader(metric.NewPeriodicReader(exporter)), metric.WithResource(resource))
	if err != nil {
		return nil, err
	}
	return provider, nil
}

func setupLoggingProvider(ctx context.Context, resource *resource.Resource) (*otellog.LoggerProvider, error) {
	exporter, err := otlploghttp.New(ctx,
		otlploghttp.WithCompression(otlploghttp.GzipCompression),
	)

	if err != nil {
		return nil, err
	}

	provider := otellog.NewLoggerProvider(
		otellog.WithProcessor(otellog.NewBatchProcessor(exporter)),
		otellog.WithResource(resource),
	)
	return provider, nil
}

// SetupOpenTelemetry bootstraps the OpenTelemetry pipeline for metrics, tracing, and logginc.
// If it does not return an error, make sure to call shutdown for proper cleanup at process exit.
// The logging provider needs to be hooked into the logger of your choice (e.g. zerolog) for logs to show
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

	traceProvider, err := setupTraceProvider(ctx, resource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}

	shutdownFuncs = append(shutdownFuncs, traceProvider.Shutdown)
	otel.SetTracerProvider(traceProvider)

	meterProvider, err := setupMetricsProvider(ctx, resource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, meterProvider.Shutdown)
	otel.SetMeterProvider(meterProvider)

	loggerProvider, err := setupLoggingProvider(ctx, resource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)

	// Pattern recommended here: https://pkg.go.dev/go.opentelemetry.io/otel/sdk/log
	// Not sure why this is in a separate "global" package rather than the normal "otel" one.
	global.SetLoggerProvider(loggerProvider)

	err = runtime.Start(runtime.WithMinimumReadMemStatsInterval(time.Second))
	if err != nil {
		handleErr(err)
	}
	return shutdown, err
}
