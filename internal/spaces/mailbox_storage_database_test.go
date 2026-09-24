package spaces_test

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/habitat-network/habitat/internal/db"
	dbtest "github.com/habitat-network/habitat/internal/db/testutil"
	notifytest "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spacestest "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// Optional PostgreSQL coverage uses a fresh schema for each test. The default
// test run still uses the repository's temporary SQLite fixture.
var storagePostgresDSN = flag.String(
	"storage-postgres-dsn",
	"",
	"disposable PostgreSQL URL for mailbox storage tests",
)

func testStorageDatabases(t *testing.T, run func(*testing.T, *gorm.DB)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) { run(t, dbtest.NewDB(t)) })
	t.Run("postgres", func(t *testing.T) {
		if *storagePostgresDSN == "" {
			t.Skip("set -storage-postgres-dsn to test a disposable PostgreSQL database")
		}
		admin, err := db.New(*storagePostgresDSN)
		require.NoError(t, err)
		adminSQL, err := admin.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, adminSQL.Close()) })
		schema := "comail_storage_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		require.NoError(t, admin.Exec("CREATE SCHEMA "+schema).Error)
		t.Cleanup(func() { require.NoError(t, admin.Exec("DROP SCHEMA "+schema+" CASCADE").Error) })
		dsn, err := url.Parse(*storagePostgresDSN)
		require.NoError(t, err)
		query := dsn.Query()
		query.Set("search_path", schema)
		dsn.RawQuery = query.Encode()
		database, err := db.New(dsn.String())
		require.NoError(t, err)
		sqlDB, err := database.DB()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
		run(t, database)
	})
}

func TestMailboxStorageConcurrentClaims(t *testing.T) {
	testStorageDatabases(t, func(t *testing.T, database *gorm.DB) {
		store := spacestest.NewTestStore(
			t,
			spacestest.WithDB(database),
			spacestest.WithNotifier(nil),
		)
		space, err := store.CreateSpace(t.Context(), orgID, "email.atmos.mailbox", "concurrent")
		require.NoError(t, err)
		const writers = 8
		batches := make([][]spaces.CreateWrite, writers)
		for i := range batches {
			value := spacestest.MustMarshalRecord(t, map[string]any{"writer": i})
			batches[i] = []spaces.CreateWrite{
				{
					Collection: "email.atmos.message",
					Rkey:       syntax.RecordKey(fmt.Sprintf("message-%d", i)),
					Value:      value,
				},
				{
					Collection: "email.atmos.messageStateOperation",
					Rkey:       "shared-claim",
					Value:      value,
				},
			}
		}
		start := make(chan struct{})
		outcomes := make(chan error, writers)
		for _, batch := range batches {
			go func() {
				<-start
				_, err := store.ApplyCreates(t.Context(), space, owner, batch)
				outcomes <- err
			}()
		}
		close(start)
		var committed, conflicts int
		for range writers {
			err := <-outcomes
			switch {
			case err == nil:
				committed++
			case errors.Is(err, spaces.ErrRecordAlreadyExists):
				conflicts++
			default:
				t.Errorf("unexpected batch error: %v", err)
			}
		}
		require.Equal(t, 1, committed)
		require.Equal(t, writers-1, conflicts)
		records, err := store.ListRecords(t.Context(), space, owner, nil)
		require.NoError(t, err)
		require.Len(t, records, 2, "losing batches must leave no orphan messages")
		require.Equal(
			t,
			records[0].Value,
			records[1].Value,
			"the message and claim must have the same winner",
		)
		rev, hash, found, err := store.RepoHead(t.Context(), space, owner)
		require.NoError(t, err)
		require.True(t, found)
		reopened := spacestest.NewTestStore(t, spacestest.WithDB(database))
		afterRev, afterHash, afterFound, err := reopened.RepoHead(t.Context(), space, owner)
		require.NoError(t, err)
		require.True(t, afterFound)
		require.Equal(t, rev, afterRev)
		require.Equal(t, hash, afterHash)
	})
}

func TestMailboxStorageRestoresLegacyBlobReferenceOnIdenticalRetry(t *testing.T) {
	testStorageDatabases(t, func(t *testing.T, database *gorm.DB) {
		notifier := &notifytest.TestNotifier{}
		store := spacestest.NewTestStore(
			t,
			spacestest.WithDB(database),
			spacestest.WithNotifier(notifier),
		)
		space, err := store.CreateSpace(t.Context(), orgID, "email.atmos.mailbox", "legacy")
		require.NoError(t, err)
		blobs := spacestest.NewTestBlobStore(t)
		blobCID, size, err := blobs.PutBlob(
			t.Context(),
			"message/rfc822",
			[]byte("synthetic legacy mail"),
		)
		require.NoError(t, err)
		require.NoError(t, store.RegisterBlobUpload(t.Context(), owner, blobCID))
		value := spacestest.MustMarshalRecord(t, map[string]any{
			"$type": "email.atmos.message",
			"blob": atdata.Blob{
				Ref:      atdata.CIDLink(blobCID),
				MimeType: "message/rfc822",
				Size:     size,
			},
		})
		_, recordCID, err := store.PutRecord(
			t.Context(),
			space,
			owner,
			"email.atmos.message",
			"legacy",
			value,
		)
		require.NoError(t, err)
		original, err := store.GetRecord(t.Context(), space, owner, "email.atmos.message", "legacy")
		require.NoError(t, err)
		rev, hash, found, err := store.RepoHead(t.Context(), space, owner)
		require.NoError(t, err)
		require.True(t, found)
		writes := len(notifier.Writes)
		// Retain the record and blob, but simulate the empty metadata tables
		// created when upgrading a provider that did not track upload ownership.
		require.NoError(t, database.Exec("DELETE FROM blob_refs").Error)
		require.NoError(t, database.Exec("DELETE FROM blob_uploads").Error)
		reopened := spacestest.NewTestStore(
			t,
			spacestest.WithDB(database),
			spacestest.WithNotifier(notifier),
		)
		_, _, err = reopened.PutRecord(
			t.Context(),
			space,
			owner,
			"email.atmos.message",
			"legacy",
			value,
		)
		require.ErrorIs(
			t,
			err,
			spaces.ErrBlobNotFound,
			"an existing record must not imply upload ownership",
		)
		referenced, err := reopened.BlobReferenced(t.Context(), space, blobCID)
		require.NoError(t, err)
		require.False(t, referenced)
		// The authenticated upload path registers the owner after receiving bytes.
		require.NoError(t, reopened.RegisterBlobUpload(t.Context(), owner, blobCID))
		_, _, err = reopened.PutRecord(
			t.Context(),
			space,
			alice,
			"email.atmos.message",
			"foreign",
			value,
		)
		require.ErrorIs(
			t,
			err,
			spaces.ErrBlobNotFound,
			"another repo cannot reuse the owner's upload",
		)
		_, retryCID, err := reopened.PutRecord(
			t.Context(),
			space,
			owner,
			"email.atmos.message",
			"legacy",
			value,
		)
		require.NoError(t, err)
		require.Equal(t, recordCID, retryCID)
		referenced, err = reopened.BlobReferenced(t.Context(), space, blobCID)
		require.NoError(t, err)
		require.True(
			t,
			referenced,
			"an authorized identical retry must restore its live blob reference",
		)
		after, err := reopened.GetRecord(t.Context(), space, owner, "email.atmos.message", "legacy")
		require.NoError(t, err)
		require.Equal(t, original, after)
		afterRev, afterHash, _, err := reopened.RepoHead(t.Context(), space, owner)
		require.NoError(t, err)
		require.Equal(t, rev, afterRev)
		require.Equal(t, hash, afterHash)
		require.Len(
			t,
			notifier.Writes,
			writes,
			"reference repair must not announce a record change",
		)
	})
}
