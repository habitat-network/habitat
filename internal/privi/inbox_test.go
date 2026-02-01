package privi

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestNotificationIngester(t *testing.T) {
	ctx := context.Background()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)

	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)

	store := NewStore(perms, repo)

	ingester, err := NewNotificationIngester(db, store)
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
		// First, create the record that will be shadowed by the notification
		shadowedRecord := map[string]any{
			"$type": "app.bsky.feed.like",
			"subject": map[string]any{
				"uri": "at://did:plc:recipient/app.bsky.feed.post/xyz789",
			},
			"createdAt": "2024-01-01T00:00:00Z",
		}
		// Access the repo through the store (we're in the same package, so we can cast)
		sqliteRepo := repo.(*sqliteRepo)
		err := sqliteRepo.PutRecord("did:plc:recipient", "app.bsky.feed.like.abc123", shadowedRecord, nil)
		require.NoError(t, err)

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
		// Verify the shadowed record value was stored
		require.NotEmpty(t, n.Value)
		var storedRecord map[string]any
		err = json.Unmarshal([]byte(n.Value), &storedRecord)
		require.NoError(t, err)
		require.Equal(t, shadowedRecord["$type"], storedRecord["$type"])
		// Verify fetch succeeded
		require.False(t, n.LastFetchFailed, "LastFetchFailed should be false when fetch succeeds")
		require.NotNil(t, n.LastSuccessfulFetch, "LastSuccessfulFetch should be set when fetch succeeds")
	})

	t.Run("creates notification with empty value when forwarding not implemented", func(t *testing.T) {
		// Test case: origin DID doesn't exist in our repo (forwarding not implemented)
		record := notificationRecord{
			Did:        "did:plc:unknown", // This DID doesn't exist in our repo
			Collection: "app.bsky.feed.like",
			Rkey:       "somekey",
		}
		recordBytes, err := json.Marshal(record)
		require.NoError(t, err)

		event := &models.Event{
			Did:  "did:plc:sender",
			Kind: models.EventKindCommit,
			Commit: &models.Commit{
				Operation:  models.CommitOperationCreate,
				Collection: "app.bsky.feed.like",
				RKey:       "somekey",
				Record:     recordBytes,
			},
		}

		err = ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify notification was created with empty value
		var notifications []Notification
		err = db.Find(&notifications).Error
		require.NoError(t, err)

		// Find the notification we just created
		var notification *Notification
		for i := range notifications {
			if notifications[i].Did == "did:plc:unknown" && notifications[i].Rkey == "somekey" {
				notification = &notifications[i]
				break
			}
		}
		require.NotNil(t, notification, "notification should be created")
		require.Equal(t, "did:plc:unknown", notification.Did)
		require.Equal(t, "did:plc:sender", notification.OriginDid)
		require.Equal(t, "app.bsky.feed.like", notification.Collection)
		require.Equal(t, "somekey", notification.Rkey)
		// Value should be empty string when forwarding not implemented
		require.Empty(t, notification.Value, "value should be empty when record fetch fails due to forwarding not implemented")
		// Verify fetch failure is tracked
		require.True(t, notification.LastFetchFailed, "LastFetchFailed should be true when fetch fails")
		require.Nil(t, notification.LastSuccessfulFetch, "LastSuccessfulFetch should be nil when fetch fails")
	})

	t.Run("creates notification with empty value when record not found", func(t *testing.T) {
		// Test case: origin DID exists but record doesn't exist
		// First ensure the DID exists by creating a different record for it
		sqliteRepo := repo.(*sqliteRepo)
		err := sqliteRepo.PutRecord("did:plc:recipient", "app.bsky.feed.like.otherkey", map[string]any{"data": "value"}, nil)
		require.NoError(t, err)

		// Now try to fetch a record that doesn't exist for this DID
		record := notificationRecord{
			Did:        "did:plc:recipient", // This DID exists in our repo
			Collection: "app.bsky.feed.like",
			Rkey:       "notfound", // But this record doesn't exist
		}
		recordBytes, err := json.Marshal(record)
		require.NoError(t, err)

		event := &models.Event{
			Did:  "did:plc:sender",
			Kind: models.EventKindCommit,
			Commit: &models.Commit{
				Operation:  models.CommitOperationCreate,
				Collection: "app.bsky.feed.like",
				RKey:       "notfound",
				Record:     recordBytes,
			},
		}

		err = ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify notification was created with empty value
		var notifications []Notification
		err = db.Find(&notifications).Error
		require.NoError(t, err)

		// Find the notification we just created
		var notification *Notification
		for i := range notifications {
			if notifications[i].Did == "did:plc:recipient" && notifications[i].Rkey == "notfound" {
				notification = &notifications[i]
				break
			}
		}
		require.NotNil(t, notification, "notification should be created")
		require.Equal(t, "did:plc:recipient", notification.Did)
		require.Equal(t, "did:plc:sender", notification.OriginDid)
		require.Equal(t, "app.bsky.feed.like", notification.Collection)
		require.Equal(t, "notfound", notification.Rkey)
		// Value should be empty string when record not found
		require.Empty(t, notification.Value, "value should be empty when record not found")
		// Verify fetch failure is tracked
		require.True(t, notification.LastFetchFailed, "LastFetchFailed should be true when record not found")
		require.Nil(t, notification.LastSuccessfulFetch, "LastSuccessfulFetch should be nil when record not found")
	})

	t.Run("updates LastFetchFailed and LastSuccessfulFetch on retry", func(t *testing.T) {
		// First, create a notification with a failed fetch
		record := notificationRecord{
			Did:        "did:plc:recipient",
			Collection: "app.bsky.feed.like",
			Rkey:       "retrytest",
		}
		recordBytes, err := json.Marshal(record)
		require.NoError(t, err)

		event := &models.Event{
			Did:  "did:plc:sender",
			Kind: models.EventKindCommit,
			Commit: &models.Commit{
				Operation:  models.CommitOperationCreate,
				Collection: "app.bsky.feed.like",
				RKey:       "retrytest",
				Record:     recordBytes,
			},
		}

		err = ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify initial state - fetch should have failed
		var notifications []Notification
		err = db.Find(&notifications).Error
		require.NoError(t, err)

		var notification *Notification
		for i := range notifications {
			if notifications[i].Rkey == "retrytest" {
				notification = &notifications[i]
				break
			}
		}
		require.NotNil(t, notification)
		require.True(t, notification.LastFetchFailed, "initial fetch should fail")
		require.Nil(t, notification.LastSuccessfulFetch, "initial fetch should not have successful timestamp")

		// Now create the record and ingest again (simulating a retry)
		sqliteRepo := repo.(*sqliteRepo)
		shadowedRecord := map[string]any{
			"$type": "app.bsky.feed.like",
			"subject": map[string]any{
				"uri": "at://did:plc:recipient/app.bsky.feed.post/xyz789",
			},
		}
		err = sqliteRepo.PutRecord("did:plc:recipient", "app.bsky.feed.like.retrytest", shadowedRecord, nil)
		require.NoError(t, err)

		// Ingest the same event again
		err = ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify updated state - fetch should now succeed
		err = db.Find(&notifications).Error
		require.NoError(t, err)

		for i := range notifications {
			if notifications[i].Rkey == "retrytest" {
				notification = &notifications[i]
				break
			}
		}
		require.NotNil(t, notification)
		require.False(t, notification.LastFetchFailed, "retry fetch should succeed")
		require.NotNil(t, notification.LastSuccessfulFetch, "retry fetch should have successful timestamp")
		require.NotEmpty(t, notification.Value, "value should be populated after successful fetch")
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
