package privi

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/rs/zerolog/log"
)

// ProcessNotification is a default handler for notification events that logs them as JSON
func ProcessNotification(ctx context.Context, event *models.Event) error {
	// Only process commit operations
	if event.Kind != models.EventKindCommit {
		return nil
	}

	// Marshal the event to JSON for logging
	eventJSON, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		log.Error().Err(err).Msg("failed to marshal event to JSON")
		return err
	}

	log.Info().
		Str("did", event.Did).
		Str("kind", event.Kind).
		RawJSON("event", eventJSON).
		Msg("received notification event")

	// TODO: Implement notification processing logic
	return nil
}
