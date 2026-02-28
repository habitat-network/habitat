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

// newInboxForTest creates a new inbox instance with an in-memory database for testing.
func newInboxForTest(t *testing.T) (Inbox, *gorm.DB) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	inbox, err := New(db)
	require.NoError(t, err)
	return inbox, db
}

func TestPutNotificationBasic(t *testing.T) {
	ctx := context.Background()
	inb, db := newInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	collection := syntax.NSID("app.bsky.feed.like")
	rkey := syntax.RecordKey("like-1")

	// Put a notification
	err := inb.Put(ctx, sender, recipient, collection, rkey, "")
	require.NoError(t, err)

	// Verify it was stored with expected values
	var n Notification
	result := db.First(&n)
	require.NoError(t, result.Error)

	require.Equal(t, sender.String(), n.Sender)
	require.Equal(t, recipient.String(), n.Recipient)
	require.Equal(t, collection.String(), n.Collection)
	require.Equal(t, rkey.String(), n.Rkey)
}

func TestPutNotificationMultipleSeparateKeys(t *testing.T) {
	ctx := context.Background()
	inb, db := newInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")

	// Put multiple notifications with different keys
	err := inb.Put(ctx, sender, recipient, "app.bsky.feed.like", "like-1", "")
	require.NoError(t, err)

	err = inb.Put(ctx, sender, recipient, "app.bsky.feed.repost", "repost-1", "")
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

func TestGetCollectionUpdatesByRecipient(t *testing.T) {
	ctx := context.Background()
	inb, _ := newInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	otherRecipient, _ := syntax.ParseDID("did:plc:other789")
	likeCollection := syntax.NSID("app.bsky.feed.like")
	repostCollection := syntax.NSID("app.bsky.feed.repost")

	// Put two likes for the target recipient
	require.NoError(t, inb.Put(ctx, sender, recipient, likeCollection, "like-1", ""))
	require.NoError(t, inb.Put(ctx, sender, recipient, likeCollection, "like-2", ""))

	// Put a repost for the target recipient (different collection)
	require.NoError(t, inb.Put(ctx, sender, recipient, repostCollection, "repost-1", ""))

	// Put a like for a different recipient
	require.NoError(t, inb.Put(ctx, sender, otherRecipient, likeCollection, "like-3", ""))

	// Get likes for the target recipient
	notifs, err := inb.GetCollectionUpdatesByRecipient(ctx, recipient, likeCollection)
	require.NoError(t, err)
	require.Len(t, notifs, 2)

	for _, n := range notifs {
		require.Equal(t, recipient.String(), n.Recipient)
		require.Equal(t, likeCollection.String(), n.Collection)
	}
}

func TestGetCollectionUpdatesByRecipientEmpty(t *testing.T) {
	ctx := context.Background()
	inb, _ := newInboxForTest(t)

	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	likeCollection := syntax.NSID("app.bsky.feed.like")

	notifs, err := inb.GetCollectionUpdatesByRecipient(ctx, recipient, likeCollection)
	require.NoError(t, err)
	require.Empty(t, notifs)
}

func TestPutNotificationSameKeyUpdatesUpdatedAt(t *testing.T) {
	ctx := context.Background()
	inb, db := newInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	collection := syntax.NSID("app.bsky.feed.like")
	rkey := syntax.RecordKey("like-1")

	// Put first notification
	err := inb.Put(ctx, sender, recipient, collection, rkey, "")
	require.NoError(t, err)

	// Get the first notification's UpdatedAt time
	var n1 Notification
	result := db.First(&n1)
	require.NoError(t, result.Error)
	firstUpdatedAt := n1.UpdatedAt

	// Wait a bit to ensure time difference
	time.Sleep(100 * time.Millisecond)

	// Put the same notification again (same sender, recipient, collection, rkey)
	err = inb.Put(ctx, sender, recipient, collection, rkey, "")
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

func TestPutNotificationReason(t *testing.T) {
	ctx := context.Background()
	inb, _ := newInboxForTest(t)

	sender, _ := syntax.ParseDID("did:plc:sender123")
	recipient, _ := syntax.ParseDID("did:plc:recipient456")
	collection := syntax.NSID("app.bsky.feed.like")
	rkey := syntax.RecordKey("like-1")

	// Put a notification
	err := inb.Put(ctx, sender, recipient, collection, rkey, "test-reason")
	require.NoError(t, err)

	// Verify it was stored with expected values
	notifs, err := inb.GetUpdatesForClique(t.Context(), recipient, "test-reason")
	require.NoError(t, err)
	require.Len(t, notifs, 1)
	n := notifs[0]

	require.Equal(t, sender.String(), n.Sender)
	require.Equal(t, recipient.String(), n.Recipient)
	require.Equal(t, collection.String(), n.Collection)
	require.Equal(t, rkey.String(), n.Rkey)
}
