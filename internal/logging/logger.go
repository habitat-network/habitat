package logging

import (
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func NewLogger() *zerolog.Logger {
	out := zerolog.NewConsoleWriter()
	logger := zerolog.New(out).With().Timestamp().Logger()
	log.Logger = logger
	return &logger
}
