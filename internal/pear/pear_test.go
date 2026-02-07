package pear

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/userstore"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A unit test testing putRecord and getRecord with one basic permission.
// TODO: an integration test with two PDS's + pear servers running.
func TestControllerPrivateDataPutGet(t *testing.T) {
	// The val the caller is trying to put
	val := map[string]any{
		"someKey": "someVal",
	}

	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	dummy, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	p := newPermissionEnforcingRepo(dummy, repo)

	// Ensure user exists before putting records
	err = userStore.EnsureUser(syntax.DID("my-did"))
	require.NoError(t, err)

	// putRecord
	coll := "my.fake.collection"
	rkey := "my-rkey"
	validate := true
	err = p.putRecord("my-did", coll, val, rkey, &validate)
	require.NoError(t, err)

	// Owner can always access their own records
	got, err := p.getRecord(coll, rkey, "my-did", "my-did")
	require.NoError(t, err)
	require.NotNil(t, got)

	var ownerUnmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &ownerUnmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, ownerUnmarshalled)

	// Non-owner without permission gets unauthorized
	got, err = p.getRecord(coll, rkey, "my-did", "another-did")
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	// Grant permission
	require.NoError(t, dummy.AddLexiconReadPermission([]string{"another-did"}, "my-did", coll))

	// Now non-owner can access
	got, err = p.getRecord(coll, "my-rkey", "my-did", "another-did")
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &unmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, unmarshalled)

	err = p.putRecord("my-did", coll, val, rkey, &validate)
	require.NoError(t, err)
}

func TestListOwnRecords(t *testing.T) {
	val := map[string]any{
		"someKey": "someVal",
	}
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	dummy, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	p := newPermissionEnforcingRepo(dummy, repo)

	// Migrate Notification table (required for listRecords)
	err = db.AutoMigrate(&Notification{})
	require.NoError(t, err)

	// Ensure user exists before putting records
	err = userStore.EnsureUser(syntax.DID("my-did"))
	require.NoError(t, err)

	// putRecord
	coll := "my.fake.collection"
	rkey := "my-rkey"
	validate := true
	err = p.putRecord("my-did", coll, val, rkey, &validate)
	require.NoError(t, err)

	records, err := p.listRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: "my-did"},
		"my-did",
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestGetRecordForwardingNotImplemented(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	p := newPermissionEnforcingRepo(perms, repo)

	// Try to get a record for a DID that doesn't exist on this server
	got, err := p.getRecord("some.collection", "some-rkey", "did:plc:unknown123", "did:plc:caller456")
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrNotLocalRepo)
}

func TestListRecordsForwardingNotImplemented(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	p := newPermissionEnforcingRepo(perms, repo)

	// Try to list records for a DID that doesn't exist on this server
	records, err := p.listRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: "some.collection", Repo: "did:plc:unknown123"},
		"did:plc:caller456",
	)
	require.Nil(t, records)
	require.ErrorIs(t, err, ErrNotLocalRepo)
}

func TestListRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	p := newPermissionEnforcingRepo(perms, repo)

	// Migrate Notification table (required for listRecords)
	err = db.AutoMigrate(&Notification{})
	require.NoError(t, err)

	val := map[string]any{"someKey": "someVal"}
	validate := true

	// Ensure users exist before putting records
	err = userStore.EnsureUser(syntax.DID("my-did"))
	require.NoError(t, err)
	err = userStore.EnsureUser(syntax.DID("other-did"))
	require.NoError(t, err)
	err = userStore.EnsureUser(syntax.DID("reader-did"))
	require.NoError(t, err)
	err = userStore.EnsureUser(syntax.DID("specific-reader"))
	require.NoError(t, err)

	// Create multiple records across collections
	coll1 := "my.fake.collection1"
	coll2 := "my.fake.collection2"

	require.NoError(t, p.putRecord("my-did", coll1, val, "rkey1", &validate))
	require.NoError(t, p.putRecord("my-did", coll1, val, "rkey2", &validate))
	require.NoError(t, p.putRecord("my-did", coll2, val, "rkey3", &validate))

	t.Run("returns empty without permissions", func(t *testing.T) {
		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "other-did"},
			"other-did",
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns records with wildcard permission", func(t *testing.T) {
		require.NoError(
			t,
			perms.AddLexiconReadPermission(
				[]string{"reader-did"},
				"my-did",
				fmt.Sprintf("%s.*", coll1),
			),
		)

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "reader-did"},
			"reader-did",
		)
		require.NoError(t, err)
		// reader-did has no own records, but has permission to see my-did's records via notifications
		// However, there are no notifications set up, so should be empty
		require.Empty(t, records)
	})

	t.Run("returns only specific permitted record", func(t *testing.T) {
		require.NoError(
			t,
			perms.AddLexiconReadPermission(
				[]string{"specific-reader"},
				"my-did",
				fmt.Sprintf("%s.rkey1", coll1),
			),
		)

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "specific-reader"},
			"specific-reader",
		)
		require.NoError(t, err)
		// specific-reader has no own records and no notifications, so should be empty
		require.Empty(t, records)
	})

	t.Run("permissions are scoped to collection", func(t *testing.T) {
		// reader-did has permission for coll1 but not coll2
		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll2, Repo: "reader-did"},
			"reader-did",
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

// TODO: eventually test permissions with blobs here
func TestPearUploadAndGetBlob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	pear := newPermissionEnforcingRepo(perms, repo)

	did := "did:example:alice"
	// use an empty blob to avoid hitting sqlite3.SQLITE_LIMIT_LENGTH in test environment
	blob := []byte("this is my test blob")
	mtype := "text/plain"

	bmeta, err := pear.uploadBlob(did, blob, mtype)
	require.NoError(t, err)
	require.NotNil(t, bmeta)
	require.Equal(t, mtype, bmeta.MimeType)
	require.Equal(t, int64(len(blob)), bmeta.Size)

	m, gotBlob, err := pear.getBlob(did, bmeta.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mtype, m)
	require.Equal(t, blob, gotBlob)
}

func TestListRecordsWithNotifications(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	userStore, err := userstore.NewUserStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db, userStore)
	require.NoError(t, err)
	p := newPermissionEnforcingRepo(perms, repo)

	// Migrate Notification table
	err = db.AutoMigrate(&Notification{})
	require.NoError(t, err)

	val := map[string]any{"someKey": "someVal"}
	validate := true
	coll := "my.fake.collection"

	// Set up users
	aliceDID := "did:plc:alice"
	bobDID := "did:plc:bob"
	carolDID := "did:plc:carol"
	remoteDID := "did:plc:remote"

	require.NoError(t, userStore.EnsureUser(syntax.DID(aliceDID)))
	require.NoError(t, userStore.EnsureUser(syntax.DID(bobDID)))
	require.NoError(t, userStore.EnsureUser(syntax.DID(carolDID)))
	// remoteDID is intentionally not created to simulate a different node

	// Alice creates her own records
	require.NoError(t, p.putRecord(aliceDID, coll, val, "alice-rkey1", &validate))
	require.NoError(t, p.putRecord(aliceDID, coll, val, "alice-rkey2", &validate))

	// Bob creates records
	require.NoError(t, p.putRecord(bobDID, coll, val, "bob-rkey1", &validate))
	require.NoError(t, p.putRecord(bobDID, coll, val, "bob-rkey2", &validate))

	// Carol creates records
	require.NoError(t, p.putRecord(carolDID, coll, val, "carol-rkey1", &validate))

	// Remote user creates records (simulating records from another node)
	require.NoError(t, p.putRecord(remoteDID, coll, val, "remote-rkey1", &validate))

	t.Run("includes records from notifications when user has permission", func(t *testing.T) {
		// Grant Alice permission to read Bob's records
		require.NoError(t, perms.AddLexiconReadPermission([]string{aliceDID}, bobDID, coll))

		// Create notification for Alice about Bob's record
		err = db.Create(&Notification{
			Did:        aliceDID,
			OriginDid:  bobDID,
			Collection: coll,
			Rkey:       "bob-rkey1",
		}).Error
		require.NoError(t, err)

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: aliceDID},
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		require.Len(t, records, 3) // 2 from Alice's own repo + 1 from Bob via notification

		// Verify we have Alice's records
		aliceRecords := 0
		bobRecords := 0
		for _, record := range records {
			if record.Did == aliceDID {
				aliceRecords++
			} else if record.Did == bobDID {
				bobRecords++
				require.Equal(t, "bob-rkey1", record.Rkey)
			}
		}
		require.Equal(t, 2, aliceRecords)
		require.Equal(t, 1, bobRecords)
	})

	t.Run("excludes records from notifications when user lacks permission", func(t *testing.T) {
		// Create notification for Alice about Carol's record (but no permission granted)
		err = db.Create(&Notification{
			Did:        aliceDID,
			OriginDid:  carolDID,
			Collection: coll,
			Rkey:       "carol-rkey1",
		}).Error
		require.NoError(t, err)

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: aliceDID},
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should still be 3 (2 from Alice + 1 from Bob with permission, but NOT Carol's)
		require.Len(t, records, 3)

		// Verify Carol's record is not included
		for _, record := range records {
			require.NotEqual(t, carolDID, record.Did, "Carol's record should not be included without permission")
		}
	})

	t.Run("skips records from different nodes", func(t *testing.T) {
		// Grant Alice permission to read remote user's records
		require.NoError(t, perms.AddLexiconReadPermission([]string{aliceDID}, remoteDID, coll))

		// Create notification for Alice about remote user's record
		err = db.Create(&Notification{
			Did:        aliceDID,
			OriginDid:  remoteDID,
			Collection: coll,
			Rkey:       "remote-rkey1",
		}).Error
		require.NoError(t, err)

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: aliceDID},
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should still be 3 (remote record should be skipped)
		require.Len(t, records, 3)

		// Verify remote record is not included
		for _, record := range records {
			require.NotEqual(t, remoteDID, record.Did, "Remote record should be skipped")
		}
	})

	t.Run("filters notifications by collection", func(t *testing.T) {
		otherColl := "other.collection"
		require.NoError(t, p.putRecord(bobDID, otherColl, val, "bob-other-rkey", &validate))
		require.NoError(t, perms.AddLexiconReadPermission([]string{aliceDID}, bobDID, otherColl))

		// Create notification for different collection
		err = db.Create(&Notification{
			Did:        aliceDID,
			OriginDid:  bobDID,
			Collection: otherColl,
			Rkey:       "bob-other-rkey",
		}).Error
		require.NoError(t, err)

		// Query for original collection
		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: aliceDID},
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should still be 3 (other collection notification should be filtered out)
		require.Len(t, records, 3)

		// Query for other collection
		records, err = p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: otherColl, Repo: aliceDID},
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should have 1 record from notification (Alice doesn't have own records in otherColl)
		require.Len(t, records, 1)
		require.Equal(t, bobDID, records[0].Did)
		require.Equal(t, "bob-other-rkey", records[0].Rkey)
	})

}
