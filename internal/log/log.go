package log

import (
	"log/slog"
	"os"
	"strings"

	"github.com/lmittmann/tint"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel/log/global"
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
		logHandlers = append(logHandlers,
			tint.NewTextHandler(os.Stdout, &tint.Options{
				AddSource: true,
				Level:     cfg.level,
			}),
		)
	}
	slog.SetLogLoggerLevel(cfg.level)
	return slog.New(slog.NewMultiHandler(logHandlers...))
}
