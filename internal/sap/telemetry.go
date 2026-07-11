package sap

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// metrics tracks the performance of sap's background goroutines: the
// crawler (one goroutine per org crawl), the subscriber (one long-lived
// goroutine per org subscription), and the resyncer (a dispatcher plus a
// fixed pool of worker goroutines).
type metrics struct {
	tracer trace.Tracer

	crawlsActive    atomic.Int64
	crawlsGauge     metric.Int64Gauge
	crawlDuration   metric.Float64Histogram
	crawlsCompleted metric.Int64Counter

	subscriptionsActive atomic.Int64
	subscriptionsGauge  metric.Int64Gauge
	subscriptionErrors  metric.Int64Counter
	eventsProcessed     metric.Int64Counter

	resyncWorkersBusy      atomic.Int64
	resyncWorkersBusyGauge metric.Int64Gauge
	resyncJobsProcessed    metric.Int64Counter
	resyncJobDuration      metric.Float64Histogram
	dispatchDuration       metric.Float64Histogram
	repoVerifications      metric.Int64Counter
}

func newMetrics(meter metric.Meter, tracer trace.Tracer) (*metrics, error) {
	if meter == nil {
		meter = metricnoop.NewMeterProvider().Meter("sap")
	}
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("sap")
	}

	crawlsGauge, err := meter.Int64Gauge(
		"sap.crawler.active",
		metric.WithUnit("item"),
		metric.WithDescription("number of org crawl goroutines currently running"),
	)
	if err != nil {
		return nil, err
	}
	crawlDuration, err := meter.Float64Histogram(
		"sap.crawler.duration",
		metric.WithUnit("s"),
		metric.WithDescription("duration of a full org crawl"),
	)
	if err != nil {
		return nil, err
	}
	crawlsCompleted, err := meter.Int64Counter(
		"sap.crawler.completed",
		metric.WithUnit("item"),
		metric.WithDescription("number of org crawls completed, by status"),
	)
	if err != nil {
		return nil, err
	}

	subscriptionsGauge, err := meter.Int64Gauge(
		"sap.subscriber.active",
		metric.WithUnit("item"),
		metric.WithDescription("number of active org subscription goroutines"),
	)
	if err != nil {
		return nil, err
	}
	subscriptionErrors, err := meter.Int64Counter(
		"sap.subscriber.errors",
		metric.WithUnit("item"),
		metric.WithDescription("number of subscription connection errors"),
	)
	if err != nil {
		return nil, err
	}
	eventsProcessed, err := meter.Int64Counter(
		"sap.subscriber.events_processed",
		metric.WithUnit("item"),
		metric.WithDescription("number of space events processed by subscriptions, by status"),
	)
	if err != nil {
		return nil, err
	}

	resyncWorkersBusyGauge, err := meter.Int64Gauge(
		"sap.resyncer.workers_busy",
		metric.WithUnit("item"),
		metric.WithDescription("number of resync worker goroutines currently processing a job"),
	)
	if err != nil {
		return nil, err
	}
	resyncJobsProcessed, err := meter.Int64Counter(
		"sap.resyncer.jobs_processed",
		metric.WithUnit("item"),
		metric.WithDescription("number of resync jobs processed, by status"),
	)
	if err != nil {
		return nil, err
	}
	resyncJobDuration, err := meter.Float64Histogram(
		"sap.resyncer.job_duration",
		metric.WithUnit("s"),
		metric.WithDescription("duration of a single resync job"),
	)
	if err != nil {
		return nil, err
	}
	dispatchDuration, err := meter.Float64Histogram(
		"sap.resyncer.dispatch_duration",
		metric.WithUnit("s"),
		metric.WithDescription("duration of a dispatch pass that claims and queues resync jobs"),
	)
	if err != nil {
		return nil, err
	}
	repoVerifications, err := meter.Int64Counter(
		"sap.resyncer.verifications",
		metric.WithUnit("item"),
		metric.WithDescription("number of repo hash verifications at head of a full pull, by result"),
	)
	if err != nil {
		return nil, err
	}

	return &metrics{
		tracer:                 tracer,
		crawlsGauge:            crawlsGauge,
		crawlDuration:          crawlDuration,
		crawlsCompleted:        crawlsCompleted,
		subscriptionsGauge:     subscriptionsGauge,
		subscriptionErrors:     subscriptionErrors,
		eventsProcessed:        eventsProcessed,
		resyncWorkersBusyGauge: resyncWorkersBusyGauge,
		resyncJobsProcessed:    resyncJobsProcessed,
		resyncJobDuration:      resyncJobDuration,
		dispatchDuration:       dispatchDuration,
		repoVerifications:      repoVerifications,
	}, nil
}

func (m *metrics) crawlStarted(ctx context.Context) {
	m.crawlsGauge.Record(ctx, m.crawlsActive.Add(1))
}

func (m *metrics) crawlFinished(ctx context.Context, start time.Time, status string) {
	m.crawlsGauge.Record(ctx, m.crawlsActive.Add(-1))
	m.crawlDuration.Record(ctx, time.Since(start).Seconds())
	m.crawlsCompleted.Add(ctx, 1, metric.WithAttributeSet(
		attribute.NewSet(attribute.String("status", status)),
	))
}

func (m *metrics) subscriptionStarted(ctx context.Context) {
	m.subscriptionsGauge.Record(ctx, m.subscriptionsActive.Add(1))
}

func (m *metrics) subscriptionEnded(ctx context.Context) {
	m.subscriptionsGauge.Record(ctx, m.subscriptionsActive.Add(-1))
}

func (m *metrics) subscriptionError(ctx context.Context) {
	m.subscriptionErrors.Add(ctx, 1)
}

func (m *metrics) eventProcessed(ctx context.Context, status string) {
	m.eventsProcessed.Add(ctx, 1, metric.WithAttributeSet(
		attribute.NewSet(attribute.String("status", status)),
	))
}

func (m *metrics) resyncWorkerBusy(ctx context.Context) {
	m.resyncWorkersBusyGauge.Record(ctx, m.resyncWorkersBusy.Add(1))
}

func (m *metrics) resyncWorkerIdle(ctx context.Context) {
	m.resyncWorkersBusyGauge.Record(ctx, m.resyncWorkersBusy.Add(-1))
}

func (m *metrics) resyncJobFinished(ctx context.Context, start time.Time, status string) {
	m.resyncJobDuration.Record(ctx, time.Since(start).Seconds())
	m.resyncJobsProcessed.Add(ctx, 1, metric.WithAttributeSet(
		attribute.NewSet(attribute.String("status", status)),
	))
}

func (m *metrics) dispatchFinished(ctx context.Context, start time.Time) {
	m.dispatchDuration.Record(ctx, time.Since(start).Seconds())
}

func (m *metrics) repoVerified(ctx context.Context, result string) {
	m.repoVerifications.Add(ctx, 1, metric.WithAttributeSet(
		attribute.NewSet(attribute.String("result", result)),
	))
}

// detachSpan returns a context that carries the trace span from ctx as a
// remote parent but is not bound to ctx's cancellation or deadline. Use
// this when spawning fire-and-forget goroutines that should appear in the
// same trace but will outlive the calling span and need no parent
// cancellation (e.g. goroutines previously started with context.Background).
func detachSpan(ctx context.Context) context.Context {
	return trace.ContextWithRemoteSpanContext(
		context.Background(),
		trace.SpanContextFromContext(ctx),
	)
}

// inheritCancelDetachSpan returns a context that inherits cancellation from
// ctx but starts a fresh trace scope: the active local span is stripped and
// re-attached as a remote parent, so the goroutine's tracer.Start creates a
// new root span linked to the calling span rather than a child of it. Use
// this for long-lived goroutines (subscriptions, crawls) that must be
// cancelled with ctx but should not show as children of a span that ends
// immediately after spawning them.
func inheritCancelDetachSpan(ctx context.Context) context.Context {
	spanCtx := trace.SpanContextFromContext(ctx)
	return trace.ContextWithRemoteSpanContext(
		trace.ContextWithSpan(ctx, tracenoop.Span{}),
		spanCtx,
	)
}
