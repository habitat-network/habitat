package syncer

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

// metrics tracks the sync engine's dispatcher, worker pool, and verification
// outcomes.
type metrics struct {
	tracer trace.Tracer

	workersBusy      atomic.Int64
	workersBusyGauge metric.Int64Gauge
	jobsProcessed    metric.Int64Counter
	jobDuration      metric.Float64Histogram
	dispatchDuration metric.Float64Histogram
	verifications    metric.Int64Counter
}

// NewMetrics builds the engine's instruments. A nil meter or tracer falls back
// to noop implementations.
func NewMetrics(meter metric.Meter, tracer trace.Tracer) (*metrics, error) {
	if meter == nil {
		meter = metricnoop.NewMeterProvider().Meter("sap")
	}
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("sap")
	}

	workersBusyGauge, err := meter.Int64Gauge(
		"sap.syncer.workers_busy",
		metric.WithUnit("item"),
		metric.WithDescription("number of sync worker goroutines currently processing a job"),
	)
	if err != nil {
		return nil, err
	}
	jobsProcessed, err := meter.Int64Counter(
		"sap.syncer.jobs_processed",
		metric.WithUnit("item"),
		metric.WithDescription("number of sync jobs processed, by status"),
	)
	if err != nil {
		return nil, err
	}
	jobDuration, err := meter.Float64Histogram(
		"sap.syncer.job_duration",
		metric.WithUnit("s"),
		metric.WithDescription("duration of a single sync job"),
	)
	if err != nil {
		return nil, err
	}
	dispatchDuration, err := meter.Float64Histogram(
		"sap.syncer.dispatch_duration",
		metric.WithUnit("s"),
		metric.WithDescription("duration of a dispatch pass that claims and queues sync jobs"),
	)
	if err != nil {
		return nil, err
	}
	verifications, err := meter.Int64Counter(
		"sap.syncer.verifications",
		metric.WithUnit("item"),
		metric.WithDescription("number of repo commit verifications, by result"),
	)
	if err != nil {
		return nil, err
	}

	return &metrics{
		tracer:           tracer,
		workersBusyGauge: workersBusyGauge,
		jobsProcessed:    jobsProcessed,
		jobDuration:      jobDuration,
		dispatchDuration: dispatchDuration,
		verifications:    verifications,
	}, nil
}

func (m *metrics) workerBusy(ctx context.Context) {
	m.workersBusyGauge.Record(ctx, m.workersBusy.Add(1))
}

func (m *metrics) workerIdle(ctx context.Context) {
	m.workersBusyGauge.Record(ctx, m.workersBusy.Add(-1))
}

func (m *metrics) jobFinished(ctx context.Context, start time.Time, status string) {
	m.jobDuration.Record(ctx, time.Since(start).Seconds())
	m.jobsProcessed.Add(ctx, 1, metric.WithAttributeSet(
		attribute.NewSet(attribute.String("status", status)),
	))
}

func (m *metrics) dispatchFinished(ctx context.Context, start time.Time) {
	m.dispatchDuration.Record(ctx, time.Since(start).Seconds())
}

func (m *metrics) verified(ctx context.Context, result string) {
	m.verifications.Add(ctx, 1, metric.WithAttributeSet(
		attribute.NewSet(attribute.String("result", result)),
	))
}
