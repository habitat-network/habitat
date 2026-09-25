package spaces_test

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	dbtest "github.com/habitat-network/habitat/internal/db/testutil"
	notifytest "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spacestest "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/stretchr/testify/require"
)

// Exercise the ordinary store entry points as well as the new batch API: an
// operation claim is useful only if another write path cannot replace it.
func TestMailboxAppendOnlyAcrossMutationPaths(t *testing.T) {
	for _, collection := range []syntax.NSID{
		"email.atmos.message", "email.atmos.messageStateRevision", "email.atmos.messageStateOperation",
		"email.atmos.folderRevision", "email.atmos.folderOperation",
		"email.atmos.messagesPrototype.entry",
	} {
		t.Run(collection.String(), func(t *testing.T) {
			notifier := &notifytest.TestNotifier{}
			store := spacestest.NewTestStore(t, spacestest.WithNotifier(notifier))
			uri, err := store.CreateSpace(t.Context(), orgID, "email.atmos.mailbox", "immutable")
			require.NoError(t, err)
			original := spacestest.MustMarshalRecord(
				t,
				map[string]any{"$type": collection.String(), "value": "synthetic original"},
			)
			replacement := spacestest.MustMarshalRecord(
				t,
				map[string]any{"$type": collection.String(), "value": "synthetic replacement"},
			)
			_, cid, err := store.PutRecord(t.Context(), uri, owner, collection, "claim", original)
			require.NoError(t, err)
			rev, hash, found, err := store.RepoHead(t.Context(), uri, owner)
			require.NoError(t, err)
			require.True(t, found)
			writes := len(notifier.Writes)
			// Identical put retries remain compatible with the existing importer.
			_, retryCID, err := store.PutRecord(
				t.Context(),
				uri,
				owner,
				collection,
				"claim",
				original,
			)
			require.NoError(t, err)
			require.Equal(t, cid, retryCID)
			_, _, err = store.PutRecord(t.Context(), uri, owner, collection, "claim", replacement)
			require.Error(t, err, "ordinary put must not replace a mailbox record")
			err = store.DeleteRecord(t.Context(), uri, owner, collection, "claim")
			require.Error(t, err, "ordinary delete must not remove a mailbox record")
			results, err := store.ApplyCreates(t.Context(), uri, owner, []spaces.CreateWrite{
				{Collection: collection, Rkey: "prefix", Value: replacement},
				{Collection: collection, Rkey: "claim", Value: original},
			})
			require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
			require.Nil(t, results)
			_, err = store.GetRecord(t.Context(), uri, owner, collection, "prefix")
			require.ErrorIs(t, err, spaces.ErrRecordNotFound)
			read, err := store.GetRecord(t.Context(), uri, owner, collection, "claim")
			require.NoError(t, err)
			require.Equal(t, *cid, read.Cid)
			afterRev, afterHash, _, err := store.RepoHead(t.Context(), uri, owner)
			require.NoError(t, err)
			require.Equal(t, rev, afterRev)
			require.Equal(t, hash, afterHash)
			require.Len(
				t,
				notifier.Writes,
				writes,
				"rejections and exact retry cannot announce a new head",
			)
		})
	}
}

func TestMailboxAppendOnlyKeepsOtherCollectionsMutable(t *testing.T) {
	store := spacestest.NewTestStore(t)
	uri, err := store.CreateSpace(t.Context(), orgID, "email.atmos.mailbox", "ordinary")
	require.NoError(t, err)
	for _, text := range []string{"first", "second"} {
		_, _, err = store.PutRecord(
			t.Context(),
			uri,
			owner,
			"com.example.note",
			"note",
			spacestest.MustMarshalRecord(t, map[string]any{"text": text}),
		)
		require.NoError(t, err)
	}
	require.NoError(t, store.DeleteRecord(t.Context(), uri, owner, "com.example.note", "note"))
}

// Seed the soft-deleted row shape from a provider predating this policy. The
// public delete API must not be used to manufacture that legacy state today.
func TestMailboxAppendOnlyRejectsPreviouslyDeletedKey(t *testing.T) {
	db := dbtest.NewDB(t)
	store := spacestest.NewTestStore(t, spacestest.WithDB(db))
	uri, err := store.CreateSpace(t.Context(), orgID, "email.atmos.mailbox", "legacy-deletion")
	require.NoError(t, err)
	collection := syntax.NSID("email.atmos.messageStateOperation")
	value := spacestest.MustMarshalRecord(t, map[string]any{"value": "synthetic"})
	_, _, err = store.PutRecord(t.Context(), uri, owner, collection, "claim", value)
	require.NoError(t, err)
	require.NoError(
		t,
		db.Table("space_records").
			Where("space = ? AND repo = ? AND collection = ? AND rkey = ?", uri, owner, collection, "claim").
			Update("deleted_at", time.Now()).
			Error,
	)
	_, _, err = store.PutRecord(t.Context(), uri, owner, collection, "claim", value)
	require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
	_, err = store.ApplyCreates(
		t.Context(),
		uri,
		owner,
		[]spaces.CreateWrite{{Collection: collection, Rkey: "claim", Value: value}},
	)
	require.ErrorIs(t, err, spaces.ErrRecordAlreadyExists)
	_, err = store.GetRecord(t.Context(), uri, owner, collection, "claim")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}
