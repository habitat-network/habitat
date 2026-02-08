package inbox

import (
	"context"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// NewInboxForTest creates a new inbox instance with an in-memory database for testing.
func NewInboxForTest(t *testing.T) (Inbox, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	inbox, err := NewInbox(db)
	require.NoError(t, err)
	return inbox, db
}

func TestPutNotificationBasic(t *testing.T) {
	ctx := context.Background()
	inb, db := NewInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	collection := "app.bsky.feed.like"
	rkey := "like-1"

	// Put a notification
	err := inb.PutNotification(ctx, sender, recipient, collection, rkey)
	require.NoError(t, err)

	// Verify it was stored with expected values
	var n Notification
	result := db.First(&n)
	require.NoError(t, result.Error)

	require.Equal(t, sender.String(), n.Sender)
	require.Equal(t, recipient.String(), n.Recipient)
	require.Equal(t, collection, n.Collection)
	require.Equal(t, rkey, n.Rkey)
}

func TestPutNotificationMultipleSeparateKeys(t *testing.T) {
	ctx := context.Background()
	inb, db := NewInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")

	// Put multiple notifications with different keys
	err := inb.PutNotification(ctx, sender, recipient, "app.bsky.feed.like", "like-1")
	require.NoError(t, err)

	err = inb.PutNotification(ctx, sender, recipient, "app.bsky.feed.repost", "repost-1")
	require.NoError(t, err)

	// Both notifications should exist
	var notifications []Notification
	result := db.Find(&notifications)
	require.NoError(t, result.Error)
	require.Len(t, notifications, 2)

	// Verify each notification has the correct values
	for _, n := range notifications {
		require.Equal(t, sender.String(), n.Sender)
		require.Equal(t, recipient.String(), n.Recipient)
		require.True(t, (n.Collection == "app.bsky.feed.like" && n.Rkey == "like-1") ||
			(n.Collection == "app.bsky.feed.repost" && n.Rkey == "repost-1"))
	}
}

func TestPutNotificationSameKeyUpdatesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	inb, db := NewInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	collection := "app.bsky.feed.like"
	rkey := "like-1"

	// Put first notification
	err := inb.PutNotification(ctx, sender, recipient, collection, rkey)
	require.NoError(t, err)

	// Get the first notification's UpdatedAt time
	var n1 Notification
	result := db.First(&n1)
	require.NoError(t, result.Error)
	firstUpdatedAt := n1.UpdatedAt

	// Wait a bit to ensure time difference
	time.Sleep(100 * time.Millisecond)

	// Put the same notification again (same sender, recipient, collection, rkey)
	err = inb.PutNotification(ctx, sender, recipient, collection, rkey)
	require.NoError(t, err)

	// Verify only one notification exists (not duplicated)
	var count int64
	db.Model(&Notification{}).Count(&count)
	require.Equal(t, int64(1), count)

	// Verify UpdatedAt was updated
	var n2 Notification
	result = db.First(&n2)
	require.NoError(t, result.Error)

	require.True(t, n2.UpdatedAt.After(firstUpdatedAt),
		"UpdatedAt should be updated when putting notification with same key. Original: %v, New: %v",
		firstUpdatedAt, n2.UpdatedAt)
}
