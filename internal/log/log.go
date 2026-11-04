package log

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/trace"
)

type config struct {
	stdout bool
	level  slog.Level
}

type Option func(*config)

func WithStdout(stdout bool) Option {
	return func(o *config) {
		o.stdout = stdout
	}
}

func WithLevel(levelStr string) Option {
	return func(o *config) {
		var level slog.Level
		_ = level.UnmarshalText([]byte(strings.ToUpper(levelStr)))
		o.level = level
	}
}

func New(
	options ...Option,
) *slog.Logger {
	var cfg config
	for _, option := range options {
		option(&cfg)
	}
	var logHandlers []slog.Handler
	logHandlers = append(logHandlers, otelslog.NewHandler(
		"habitat",
		otelslog.WithLoggerProvider(global.GetLoggerProvider()),
	))
	if cfg.stdout {
		// The OTel handler links records to their span itself; stdout gets
		// the ids as attributes, to find a log line's trace.
		logHandlers = append(logHandlers, traceHandler{
			tint.NewTextHandler(os.Stdout, &tint.Options{
				AddSource: true,
				Level:     cfg.level,
			}),
		})
	}
	slog.SetLogLoggerLevel(cfg.level)
	return slog.New(slog.NewMultiHandler(logHandlers...))
}

// traceHandler adds the trace and span ids of a record's context, when it has
// a span, as the attributes trace_id and span_id.
type traceHandler struct {
	slog.Handler
}

func (h traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r = r.Clone()
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.Handler.Handle(ctx, r)
}

func (h traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return traceHandler{h.Handler.WithAttrs(attrs)}
}

func (h traceHandler) WithGroup(name string) slog.Handler {
	return traceHandler{h.Handler.WithGroup(name)}
}
