package privi

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// A unit test testing putRecord and getRecord with one basic permission.
// TODO: an integration test with two PDS's + privi servers running.
func TestControllerPrivateDataPutGet(t *testing.T) {
	// The val the caller is trying to put
	val := map[string]any{
		"someKey": "someVal",
	}

	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	dummy, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	p := newStore(dummy, repo)

	// putRecord
	coll := "my.fake.collection"
	rkey := "my-rkey"
	validate := true
	ownerDIDStr := "did:plc:owner123"
	callerDIDStr := "did:plc:caller456"
	ownerDID, err := syntax.ParseDID(ownerDIDStr)
	require.NoError(t, err)
	callerDID, err := syntax.ParseDID(callerDIDStr)
	require.NoError(t, err)
	err = p.PutRecord(ownerDIDStr, coll, val, rkey, &validate)
	require.NoError(t, err)

	// Owner can always access their own records
	got, err := p.GetRecord(coll, rkey, ownerDID, ownerDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	var ownerUnmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Rec), &ownerUnmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, ownerUnmarshalled)

	// Non-owner without permission gets unauthorized
	got, err = p.GetRecord(coll, rkey, ownerDID, callerDID)
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	// Grant permission
	require.NoError(t, dummy.AddLexiconReadPermission([]string{callerDIDStr}, ownerDIDStr, coll))

	// Now non-owner can access
	got, err = p.GetRecord(coll, "my-rkey", ownerDID, callerDID)
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Rec), &unmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, unmarshalled)

	err = p.PutRecord(ownerDIDStr, coll, val, rkey, &validate)
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
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	p := newStore(dummy, repo)

	// putRecord
	coll := "my.fake.collection"
	rkey := "my-rkey"
	validate := true
	ownerDIDStr := "did:plc:owner123"
	ownerDID, err := syntax.ParseDID(ownerDIDStr)
	require.NoError(t, err)
	err = p.PutRecord(ownerDIDStr, coll, val, rkey, &validate)
	require.NoError(t, err)

	records, err := p.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: ownerDIDStr},
		ownerDID,
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestGetRecordForwardingNotImplemented(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	p := newStore(perms, repo)

	// Try to get a record for a DID that doesn't exist on this server
	got, err := p.GetRecord("some.collection", "some-rkey", "did:plc:unknown123", "did:plc:caller456")
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrForwardingNotImplemented)
}

func TestListRecordsForwardingNotImplemented(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	p := newStore(perms, repo)

	// Try to list records for a DID that doesn't exist on this server
	records, err := p.ListRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: "some.collection", Repo: "did:plc:unknown123"},
		"did:plc:caller456",
	)
	require.Nil(t, records)
	require.ErrorIs(t, err, ErrForwardingNotImplemented)
}

func TestListRecords(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	perms, err := permissions.NewSQLiteStore(db)
	require.NoError(t, err)
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	p := newStore(perms, repo)

	val := map[string]any{"someKey": "someVal"}
	validate := true

	// Create multiple records across collections
	coll1 := "my.fake.collection1"
	coll2 := "my.fake.collection2"
	ownerDIDStr := "did:plc:owner123"
	otherDIDStr := "did:plc:other456"
	readerDIDStr := "did:plc:reader789"
	specificReaderDIDStr := "did:plc:specificreader"

	otherDID, err := syntax.ParseDID(otherDIDStr)
	require.NoError(t, err)
	readerDID, err := syntax.ParseDID(readerDIDStr)
	require.NoError(t, err)
	specificReaderDID, err := syntax.ParseDID(specificReaderDIDStr)
	require.NoError(t, err)

	require.NoError(t, p.PutRecord(ownerDIDStr, coll1, val, "rkey1", &validate))
	require.NoError(t, p.PutRecord(ownerDIDStr, coll1, val, "rkey2", &validate))
	require.NoError(t, p.PutRecord(ownerDIDStr, coll2, val, "rkey3", &validate))

	t.Run("returns empty without permissions", func(t *testing.T) {
		records, err := p.ListRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: ownerDIDStr},
			otherDID,
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns records with wildcard permission", func(t *testing.T) {
		require.NoError(
			t,
			perms.AddLexiconReadPermission(
				[]string{readerDIDStr},
				ownerDIDStr,
				fmt.Sprintf("%s.*", coll1),
			),
		)

		records, err := p.ListRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: ownerDIDStr},
			readerDID,
		)
		require.NoError(t, err)
		require.Len(t, records, 2)
	})

	t.Run("returns only specific permitted record", func(t *testing.T) {
		require.NoError(
			t,
			perms.AddLexiconReadPermission(
				[]string{specificReaderDIDStr},
				ownerDIDStr,
				fmt.Sprintf("%s.rkey1", coll1),
			),
		)

		records, err := p.ListRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: ownerDIDStr},
			specificReaderDID,
		)
		require.NoError(t, err)
		require.Len(t, records, 1)
	})

	t.Run("permissions are scoped to collection", func(t *testing.T) {
		// readerDID has permission for coll1 but not coll2
		records, err := p.ListRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll2, Repo: ownerDIDStr},
			readerDID,
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}
