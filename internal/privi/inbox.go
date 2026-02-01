package privi

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
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
	Did                 string `gorm:"uniqueIndex:idx_notification_unique,priority:1"`
	OriginDid           string `gorm:"uniqueIndex:idx_notification_unique,priority:2"`
	Collection          string `gorm:"uniqueIndex:idx_notification_unique,priority:3"`
	Rkey                string `gorm:"uniqueIndex:idx_notification_unique,priority:4"`
	Value               string
	LastFetchFailed     bool       // True if the last attempted record fetch failed
	LastSuccessfulFetch *time.Time `gorm:"type:timestamp"` // Timestamp of the last successful record fetch (nil if never successful)
}

// NotificationIngester handles the ingestion of notification events from Jetstream
type NotificationIngester struct {
	db    *gorm.DB
	store Store
}

// NewNotificationIngester creates a new NotificationIngester instance
func NewNotificationIngester(db *gorm.DB, store Store) (*NotificationIngester, error) {

	err := db.AutoMigrate(&Notification{})
	if err != nil {
		return nil, err
	}
	return &NotificationIngester{
		db:    db,
		store: store,
	}, nil
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

	// Fetch the shadowed record from the store
	origin, err := syntax.ParseDID(originDid)
	if err != nil {
		return err
	}

	caller, err := syntax.ParseDID(did)
	if err != nil {
		return err
	}
	// Query the origin repo with us as the caller
	// If the record fetch fails for any reason (forwarding not implemented, record not found,
	// unauthorized, etc.), we set the value to an empty string and still create the notification.
	// This ensures notifications are created even when we can't fetch the shadowed record.
	recordMarshalledJson := ""
	fetchFailed := false
	var lastSuccessfulFetch *time.Time

	record, err := n.store.GetRecord(collection, rkey, origin, caller)
	if err != nil {
		fetchFailed = true
		if errors.Is(err, ErrForwardingNotImplemented) {
			log.Warn().Err(err).Msgf("skipping fetch-back for %s, forwarding not implemented", originDid)
		} else if errors.Is(err, ErrRecordNotFound) {
			log.Warn().Err(err).Msgf("record not found for %s/%s/%s", originDid, collection, rkey)
		} else if errors.Is(err, ErrUnauthorized) {
			log.Warn().Err(err).Msgf("unauthorized to fetch record for %s/%s/%s", originDid, collection, rkey)
		} else {
			log.Warn().Err(err).Msgf("failed to fetch record for %s/%s/%s", originDid, collection, rkey)
		}
		// Continue with empty string value - notification will still be created
	} else {
		recordMarshalledJson = record.Rec
		now := time.Now()
		lastSuccessfulFetch = &now
	}

	return gorm.G[Notification](
		n.db,
		clause.OnConflict{
			Columns: []clause.Column{
				{Name: "did"},
				{Name: "origin_did"},
				{Name: "collection"},
				{Name: "rkey"},
			},
			DoUpdates: clause.AssignmentColumns([]string{"value", "last_fetch_failed", "last_successful_fetch", "updated_at"}),
		},
	).Create(
		context.Background(),
		&Notification{
			Did:                 did,
			OriginDid:           originDid,
			Collection:          collection,
			Rkey:                rkey,
			Value:               recordMarshalledJson,
			LastFetchFailed:     fetchFailed,
			LastSuccessfulFetch: lastSuccessfulFetch,
		},
	)
}

type Inbox struct {
	db *gorm.DB
}

func NewInbox(db *gorm.DB) *Inbox {
	return &Inbox{
		db: db,
	}
}

func (i *Inbox) getNotificationsByDid(did string) ([]Notification, error) {
	query := gorm.G[Notification](i.db).
		Where("did = ?", did).
		Order("created_at DESC")

	rows, err := query.Find(context.Background())

	if err != nil {
		return nil, err
	}
	return rows, nil
}
