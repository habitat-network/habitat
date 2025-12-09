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

func TestInbox(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	err = db.AutoMigrate(&Notification{})
	require.NoError(t, err)

	inbox := NewInbox(db)

	t.Run("returns empty list when no notifications exist", func(t *testing.T) {
		notifications, err := inbox.getNotificationsByDid("did:plc:nonexistent")
		require.NoError(t, err)
		require.Empty(t, notifications)
	})

	t.Run("returns notifications for specific did", func(t *testing.T) {
		// Create notifications for different DIDs
		db.Create(&Notification{
			Did:        "did:plc:alice",
			OriginDid:  "did:plc:bob",
			Collection: "app.bsky.feed.like",
			Rkey:       "like1",
		})
		db.Create(&Notification{
			Did:        "did:plc:alice",
			OriginDid:  "did:plc:carol",
			Collection: "app.bsky.feed.repost",
			Rkey:       "repost1",
		})
		db.Create(&Notification{
			Did:        "did:plc:bob",
			OriginDid:  "did:plc:alice",
			Collection: "app.bsky.feed.like",
			Rkey:       "like2",
		})

		// Get notifications for alice
		aliceNotifications, err := inbox.getNotificationsByDid("did:plc:alice")
		require.NoError(t, err)
		require.Len(t, aliceNotifications, 2)

		// Verify both notifications belong to alice
		for _, n := range aliceNotifications {
			require.Equal(t, "did:plc:alice", n.Did)
		}

		// Get notifications for bob
		bobNotifications, err := inbox.getNotificationsByDid("did:plc:bob")
		require.NoError(t, err)
		require.Len(t, bobNotifications, 1)
		require.Equal(t, "did:plc:bob", bobNotifications[0].Did)
		require.Equal(t, "did:plc:alice", bobNotifications[0].OriginDid)
	})
}
