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
		// The record should be in the sender's repo (originDid), not the recipient's
		shadowedRecord := map[string]any{
			"$type": "app.bsky.feed.like",
			"subject": map[string]any{
				"uri": "at://did:plc:recipient/app.bsky.feed.post/xyz789",
			},
			"createdAt": "2024-01-01T00:00:00Z",
		}
		// Access the repo through the store (we're in the same package, so we can cast)
		sqliteRepo := repo.(*sqliteRepo)
		// Put the record in the sender's repo (originDid)
		err := sqliteRepo.PutRecord("did:plc:sender", "app.bsky.feed.like.abc123", shadowedRecord, nil)
		require.NoError(t, err)
		// Grant permission to recipient to read sender's records
		err = perms.AddLexiconReadPermission([]string{"did:plc:recipient"}, "did:plc:sender", "app.bsky.feed.like")
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

	t.Run("creates notification with empty value when record fetch fails", func(t *testing.T) {
		sqliteRepo := repo.(*sqliteRepo)
		senderDID := "did:plc:sender"
		collection := "app.bsky.feed.like"

		// Setup: Create a record for unauthorized test case
		ownerDID := "did:plc:owner123"
		unauthorizedCallerDID := "did:plc:unauthorized456"
		existingRecord := map[string]any{
			"$type": "app.bsky.feed.like",
			"subject": map[string]any{
				"uri": "at://did:plc:owner123/app.bsky.feed.post/xyz789",
			},
		}
		err := sqliteRepo.PutRecord(ownerDID, "app.bsky.feed.like.authztest", existingRecord, nil)
		require.NoError(t, err)

		// Ensure ownerDID exists in repo for record not found test
		err = sqliteRepo.PutRecord(ownerDID, "app.bsky.feed.like.otherkey", map[string]any{"data": "value"}, nil)
		require.NoError(t, err)

		tests := []struct {
			name          string
			recordDid     string // The recipient (notification's Did field)
			eventDid      string // The sender (notification's OriginDid field)
			recordRkey    string
			expectedDid   string // Expected notification Did
			expectedRkey  string
			failureReason string
		}{
			{
				name:          "forwarding not implemented",
				recordDid:     "did:plc:unknown",
				eventDid:      senderDID,
				recordRkey:    "somekey",
				expectedDid:   "did:plc:unknown",
				expectedRkey:  "somekey",
				failureReason: "forwarding not implemented",
			},
			{
				name:          "record not found",
				recordDid:     ownerDID, // Recipient
				eventDid:      ownerDID, // Sender (has repo, but record doesn't exist)
				recordRkey:    "notfound",
				expectedDid:   ownerDID,
				expectedRkey:  "notfound",
				failureReason: "record not found",
			},
			{
				name:          "unauthorized",
				recordDid:     unauthorizedCallerDID, // Recipient doesn't have permission
				eventDid:      ownerDID,              // Sender owns the record
				recordRkey:    "authztest",
				expectedDid:   unauthorizedCallerDID,
				expectedRkey:  "authztest",
				failureReason: "unauthorized",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				// Create notification record
				record := notificationRecord{
					Did:        tt.recordDid,
					Collection: collection,
					Rkey:       tt.recordRkey,
				}
				recordBytes, err := json.Marshal(record)
				require.NoError(t, err)

				event := &models.Event{
					Did:  tt.eventDid,
					Kind: models.EventKindCommit,
					Commit: &models.Commit{
						Operation:  models.CommitOperationCreate,
						Collection: collection,
						RKey:       tt.recordRkey,
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
					if notifications[i].Did == tt.expectedDid && notifications[i].Rkey == tt.expectedRkey {
						notification = &notifications[i]
						break
					}
				}
				require.NotNil(t, notification, "notification should be created")
				require.Equal(t, tt.expectedDid, notification.Did)
				require.Equal(t, tt.eventDid, notification.OriginDid)
				require.Equal(t, collection, notification.Collection)
				require.Equal(t, tt.expectedRkey, notification.Rkey)
				// Value should be empty string when record fetch fails
				require.Empty(t, notification.Value, "value should be empty when record fetch fails: %s", tt.failureReason)
				// Verify fetch failure is tracked
				require.True(t, notification.LastFetchFailed, "LastFetchFailed should be true when fetch fails: %s", tt.failureReason)
				require.Nil(t, notification.LastSuccessfulFetch, "LastSuccessfulFetch should be nil when fetch fails: %s", tt.failureReason)
			})
		}
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
		// Query for the specific notification by known attributes
		var notifications []Notification
		err = db.Where("did = ? AND origin_did = ? AND collection = ? AND rkey = ?",
			"did:plc:recipient", "did:plc:sender", "app.bsky.feed.like", "retrytest").Find(&notifications).Error
		require.NoError(t, err)
		require.Len(t, notifications, 1, "should have exactly one matching notification")

		notification := notifications[0]
		require.True(t, notification.LastFetchFailed, "initial fetch should fail")
		require.Nil(t, notification.LastSuccessfulFetch, "initial fetch should not have successful timestamp")

		// Now create the record and ingest again (simulating a retry)
		// The record should be in the sender's repo (originDid), not the recipient's
		sqliteRepo := repo.(*sqliteRepo)
		shadowedRecord := map[string]any{
			"$type": "app.bsky.feed.like",
			"subject": map[string]any{
				"uri": "at://did:plc:recipient/app.bsky.feed.post/xyz789",
			},
		}
		err = sqliteRepo.PutRecord("did:plc:sender", "app.bsky.feed.like.retrytest", shadowedRecord, nil)
		require.NoError(t, err)
		// Grant permission to recipient to read sender's records
		err = perms.AddLexiconReadPermission([]string{"did:plc:recipient"}, "did:plc:sender", "app.bsky.feed.like")
		require.NoError(t, err)

		// Verify the record exists before retry
		verifyRecord, err := sqliteRepo.GetRecord("did:plc:sender", "app.bsky.feed.like.retrytest")
		require.NoError(t, err, "record should exist before retry")
		require.NotNil(t, verifyRecord)

		// Ingest the same event again
		err = ingester.Ingest(ctx, event)
		require.NoError(t, err)

		// Verify updated state - fetch should now succeed
		// Query for the specific notification by known attributes
		var updatedNotifications []Notification
		err = db.Where("did = ? AND origin_did = ? AND collection = ? AND rkey = ?",
			"did:plc:recipient", "did:plc:sender", "app.bsky.feed.like", "retrytest").Find(&updatedNotifications).Error
		require.NoError(t, err)
		require.Len(t, updatedNotifications, 1, "should have exactly one matching notification after retry")

		updatedNotification := updatedNotifications[0]
		require.False(t, updatedNotification.LastFetchFailed, "retry fetch should succeed")
		require.NotNil(t, updatedNotification.LastSuccessfulFetch, "retry fetch should have successful timestamp")
		require.NotEmpty(t, updatedNotification.Value, "value should be populated after successful fetch")
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
