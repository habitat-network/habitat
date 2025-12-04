package privi

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestListNotifications(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)

	require.NoError(t, err)
	require.NoError(t, err)
	inbox := NewInbox(db)

	// Migrate Notification table
	err = db.AutoMigrate(&Notification{})
	require.NoError(t, err)

	t.Run("returns empty list when no notifications", func(t *testing.T) {
		notifications, err := inbox.getNotificationsByDid("did:plc:alice")
		require.NoError(t, err)
		require.Empty(t, notifications)
	})

	t.Run("returns notifications for the requesting user", func(t *testing.T) {
		// Create notifications for different users
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
			Collection: "app.bsky.feed.follow",
			Rkey:       "follow1",
		})

		// Alice should see her 2 notifications
		aliceNotifications, err := inbox.getNotificationsByDid("did:plc:alice")
		require.NoError(t, err)
		require.Len(t, aliceNotifications, 2)
		for _, n := range aliceNotifications {
			require.Equal(t, "did:plc:alice", n.Did)
		}

		// Bob should see his 1 notification
		bobNotifications, err := inbox.getNotificationsByDid("did:plc:bob")
		require.NoError(t, err)
		require.Len(t, bobNotifications, 1)
		require.Equal(t, "did:plc:bob", bobNotifications[0].Did)
		require.Equal(t, "did:plc:alice", bobNotifications[0].OriginDid)

		// Carol has no notifications
		carolNotifications, err := inbox.getNotificationsByDid("did:plc:carol")
		require.NoError(t, err)
		require.Empty(t, carolNotifications)
	})
}
