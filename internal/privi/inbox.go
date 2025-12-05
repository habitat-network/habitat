package privi

import (
	"context"
	"encoding/json"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type notificationRecord struct {
	Did        string
	Collection string
	Rkey       string
}

// Notification is a Gorm model for notifications.
type Notification struct {
	gorm.Model
	Did        string
	OriginDid  string
	Collection string
	Rkey       string
}

// NotificationIngester handles the ingestion of notification events from Jetstream
type NotificationIngester struct {
	db *gorm.DB
}

// NewNotificationIngester creates a new NotificationIngester instance
func NewNotificationIngester(db *gorm.DB) *NotificationIngester {
	return &NotificationIngester{
		db: db,
	}
}

// GetEventHandler returns an EventHandler that can be used to ingest notifications
func (n *NotificationIngester) GetEventHandler() EventHandler {
	return func(ctx context.Context, event *models.Event, db *gorm.DB) error {
		return n.Ingest(ctx, event)
	}
}

// Ingest processes a notification event and stores it in the database
func (n *NotificationIngester) Ingest(ctx context.Context, event *models.Event) error {
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

	var record notificationRecord
	recordBytes, err := json.Marshal(event.Commit.Record)
	if err != nil {
		return err
	}

	err = json.Unmarshal(recordBytes, &record)
	if err != nil {
		return err
	}

	originDid := event.Did

	err = n.createNotification(record.Did, originDid, record.Collection, record.Rkey)
	if err != nil {
		log.Error().Err(err).Msg("failed to create notification")
		return err
	}

	// TODO: Implement notification processing logic
	return nil
}

func (n *NotificationIngester) createNotification(did string, originDid string, collection string, rkey string) error {
	log.Info().
		Str("did", did).
		Str("originDid", originDid).
		Str("collection", collection).
		Str("rkey", rkey).
		Msg("creating notification")

	return gorm.G[Notification](
		n.db,
		clause.OnConflict{UpdateAll: true},
	).Create(
		context.Background(),
		&Notification{
			Did:        did,
			OriginDid:  originDid,
			Collection: collection,
			Rkey:       rkey,
		},
	)
}
