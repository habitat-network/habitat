package privi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNotificationIngester(t *testing.T) {
	ctx := context.Background()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	ingester, err := NewNotificationIngester(db)
	require.NoError(t, err)

	t.Run("ignores non-commit events", func(t *testing.T) {
		event := &models.Event{
			Did:  "did:plc:test123",
			Kind: models.EventKindIdentity,
		}

		err := ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify no notifications were created
		var count int64
		db.Model(&Notification{}).Count(&count)
		require.Equal(t, int64(0), count)
	})

	t.Run("ingests commit events and creates notification", func(t *testing.T) {
		record := notificationRecord{
			Did:        "did:plc:recipient",
			Collection: "app.bsky.feed.like",
			Rkey:       "abc123",
		}
		recordBytes, err := json.Marshal(record)
		require.NoError(t, err)

		event := &models.Event{
			Did:  "did:plc:sender",
			Kind: models.EventKindCommit,
			Commit: &models.Commit{
				Operation:  models.CommitOperationCreate,
				Collection: "app.bsky.feed.like",
				RKey:       "abc123",
				Record:     recordBytes,
			},
		}

		err = ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify notification was created
		var notifications []Notification
		err = db.Find(&notifications).Error
		require.NoError(t, err)
		require.Len(t, notifications, 1)

		n := notifications[0]
		require.Equal(t, "did:plc:recipient", n.Did)
		require.Equal(t, "did:plc:sender", n.OriginDid)
		require.Equal(t, "app.bsky.feed.like", n.Collection)
		require.Equal(t, "abc123", n.Rkey)
	})

}
