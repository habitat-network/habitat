package pear

import (
	"context"
	"log/slog"
	"time"

	"github.com/bluesky-social/jetstream/pkg/client"
	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// EventHandler is a function that processes Jetstream events
type EventHandler func(ctx context.Context, event *models.Event, db *gorm.DB) error

// simpleScheduler is a basic scheduler that immediately processes events
type simpleScheduler struct {
	handler EventHandler
	ctx     context.Context
	db      *gorm.DB
}

func (s *simpleScheduler) AddWork(ctx context.Context, repo string, evt *models.Event) error {
	// Call the user-provided handler directly
	if s.handler != nil {
		return s.handler(s.ctx, evt, s.db)
	}
	return nil
}

func (s *simpleScheduler) Shutdown() {
	// Nothing to clean up for simple scheduler
}

// StartNotificationListener connects to the ATProto Jetstream and listens for events
// in the specified collections. This function is designed to run in its own goroutine.
func StartNotificationListener(
	ctx context.Context,
	config *client.ClientConfig,
	cursor *int64,
	handler EventHandler,
	db *gorm.DB,
) error {
	log.Info().
		Str("url", config.WebsocketURL).
		Strs("collections", config.WantedCollections).
		Msg("starting jetstream listener")

	// Create a slog logger adapter for the Jetstream client
	// Jetstream expects slog, but we use zerolog throughout the codebase
	slogger := slog.New(slog.NewJSONHandler(log.Logger, nil))

	// Create a simple scheduler that passes events to the handler
	scheduler := &simpleScheduler{
		handler: handler,
		ctx:     ctx,
		db:      db,
	}

	// Create the Jetstream client
	jsClient, err := client.NewClient(config, slogger, scheduler)
	if err != nil {
		return err
	}

	// Reconnection loop with exponential backoff
	backoff := time.Second
	maxBackoff := 60 * time.Second

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("jetstream listener shutting down")
			scheduler.Shutdown()
			return ctx.Err()
		default:
			// Connect and read from Jetstream
			err := jsClient.ConnectAndRead(ctx, cursor)

			if err != nil && ctx.Err() == nil {
				// Only log and retry if context wasn't cancelled
				log.Error().
					Err(err).
					Dur("backoff", backoff).
					Msg("jetstream connection error, reconnecting")

				// Wait before reconnecting with exponential backoff
				select {
				case <-ctx.Done():
					scheduler.Shutdown()
					return ctx.Err()
				case <-time.After(backoff):
					// Increase backoff, but cap at maxBackoff
					backoff = backoff * 2
					if backoff > maxBackoff {
						backoff = maxBackoff
					}
					continue
				}
			} else if err != nil {
				// Context was cancelled
				scheduler.Shutdown()
				return err
			}

			// Connection closed cleanly, reset backoff
			backoff = time.Second
		}
	}
}
