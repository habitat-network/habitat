package search

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/meilisearch/meilisearch-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"
)

const instrumentationName = "github.com/habitat-network/habitat/internal/search"

var tracer = otel.Tracer(instrumentationName)

// Attribute keys for search spans and metrics. Only the low-cardinality ones
// (job kind, operation, outcome, mode) are used as metric attributes.
const (
	jobKindKey    = attribute.Key("habitat.search.job.kind")
	operationKey  = attribute.Key("habitat.search.operation")
	outcomeKey    = attribute.Key("habitat.search.outcome")
	modeKey       = attribute.Key("habitat.search.mode")
	taskUIDKey    = attribute.Key("meilisearch.task.uid")
	taskStatusKey = attribute.Key("meilisearch.task.status")
)

// durationBuckets are histogram boundaries in seconds, from a fast
// Meilisearch write up to an indexer that is minutes behind.
var durationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60, 120, 300, 600,
}

// metrics are the search package's instruments. They come from the global
// meter provider, like the tracer, so they report once telemetry is set up.
type metrics struct {
	// meter is the meter the instruments come from, which callbacks for the
	// observable ones register with.
	meter metric.Meter

	queueSize    metric.Int64ObservableGauge
	queueWait    metric.Float64Histogram
	jobDuration  metric.Float64Histogram
	indexLag     metric.Float64Histogram
	records      metric.Int64Counter
	taskDuration metric.Float64Histogram
	taskQueued   metric.Float64Histogram
	query        metric.Float64Histogram
}

var getMetrics = sync.OnceValue(func() *metrics {
	meter := otel.Meter(instrumentationName)
	var errs []error
	check := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	seconds := func(desc string) []metric.Float64HistogramOption {
		return []metric.Float64HistogramOption{
			metric.WithUnit("s"),
			metric.WithDescription(desc),
			metric.WithExplicitBucketBoundaries(durationBuckets...),
		}
	}
	m := &metrics{meter: meter}
	var err error
	m.queueSize, err = meter.Int64ObservableGauge("search.indexer.queue.size",
		metric.WithUnit("{job}"),
		metric.WithDescription("indexing jobs queued or running"))
	check(err)
	m.queueWait, err = meter.Float64Histogram("search.indexer.queue.wait",
		seconds("time an indexing job waited in the queue before it ran")...)
	check(err)
	m.jobDuration, err = meter.Float64Histogram("search.indexer.job.duration",
		seconds("time an indexing job took to run")...)
	check(err)
	m.indexLag, err = meter.Float64Histogram("search.indexer.lag",
		seconds("time from a record's write to its being searchable")...)
	check(err)
	m.records, err = meter.Int64Counter("search.indexer.records",
		metric.WithUnit("{record}"),
		metric.WithDescription("records written to or deleted from the search index"))
	check(err)
	m.taskDuration, err = meter.Float64Histogram("search.meilisearch.task.duration",
		seconds("time from a Meilisearch task's enqueueing to its finishing")...)
	check(err)
	m.taskQueued, err = meter.Float64Histogram("search.meilisearch.task.queued",
		seconds("time a Meilisearch task waited in Meilisearch's queue before it started")...)
	check(err)
	m.query, err = meter.Float64Histogram("search.query.duration",
		seconds("time to answer a search, including resolving the caller and loading records")...)
	check(err)
	if err := errors.Join(errs...); err != nil {
		// Instruments are still usable when creating one fails; report it
		// through the OTel error handler rather than failing search.
		otel.Handle(err)
	}
	return m
})

// outcome is the outcome attribute of an operation that returned err.
func outcome(err error) attribute.KeyValue {
	if err != nil {
		return outcomeKey.String("error")
	}
	return outcomeKey.String("ok")
}

// taskOptions tags a Meilisearch write with the W3C traceparent of the span
// in ctx, as the task's customMetadata. Meilisearch keeps it on the task, so
// a task found through GET /tasks leads back to the trace that wrote it.
func taskOptions(ctx context.Context) *meilisearch.DocumentOptions {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	traceparent := carrier.Get("traceparent")
	if traceparent == "" {
		return nil
	}
	return &meilisearch.DocumentOptions{TaskCustomMetadata: traceparent}
}

// recordTask records a finished Meilisearch task on the span in ctx and in
// the task metrics.
func recordTask(ctx context.Context, task *meilisearch.Task) {
	attrs := []attribute.KeyValue{
		taskUIDKey.Int64(task.UID),
		taskStatusKey.String(string(task.Status)),
		semconv.DBOperationName(string(task.Type)),
	}
	trace.SpanFromContext(ctx).AddEvent("meilisearch.task", trace.WithAttributes(attrs...))
	if task.EnqueuedAt.IsZero() || task.FinishedAt.IsZero() {
		return
	}
	set := metric.WithAttributes(
		semconv.DBOperationName(string(task.Type)),
		taskStatusKey.String(string(task.Status)),
	)
	m := getMetrics()
	m.taskDuration.Record(ctx, task.FinishedAt.Sub(task.EnqueuedAt).Seconds(), set)
	if !task.StartedAt.IsZero() {
		m.taskQueued.Record(ctx, task.StartedAt.Sub(task.EnqueuedAt).Seconds(), set)
	}
}

// secondsSince is the time from t to now in seconds, for histograms.
func secondsSince(t time.Time) float64 {
	return time.Since(t).Seconds()
}
