package privi

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/eagraf/habitat-new/api/habitat"
	"github.com/eagraf/habitat-new/internal/permissions"
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
	err = p.putRecord("my-did", coll, val, rkey, &validate)
	require.NoError(t, err)

	// Owner can always access their own records
	got, err := p.getRecord(coll, rkey, "my-did", "my-did")
	require.NoError(t, err)
	require.NotNil(t, got)

	var ownerUnmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Rec), &ownerUnmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, ownerUnmarshalled)

	// Non-owner without permission gets unauthorized
	got, err = p.getRecord(coll, rkey, "my-did", "another-did")
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	// Grant permission
	require.NoError(t, dummy.AddLexiconReadPermission("another-did", "my-did", coll))

	// Now non-owner can access
	got, err = p.getRecord(coll, "my-rkey", "my-did", "another-did")
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Rec), &unmarshalled)
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
	repo, err := NewSQLiteRepo(db)
	require.NoError(t, err)
	p := newStore(dummy, repo)

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

	require.NoError(t, p.putRecord("my-did", coll1, val, "rkey1", &validate))
	require.NoError(t, p.putRecord("my-did", coll1, val, "rkey2", &validate))
	require.NoError(t, p.putRecord("my-did", coll2, val, "rkey3", &validate))

	t.Run("returns empty without permissions", func(t *testing.T) {
		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "my-did"},
			"other-did",
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns records with wildcard permission", func(t *testing.T) {
		require.NoError(t, perms.AddLexiconReadPermission("reader-did", "my-did", fmt.Sprintf("%s.*", coll1)))

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "my-did"},
			"reader-did",
		)
		require.NoError(t, err)
		require.Len(t, records, 2)
	})

	t.Run("returns only specific permitted record", func(t *testing.T) {
		require.NoError(t, perms.AddLexiconReadPermission("specific-reader", "my-did", fmt.Sprintf("%s.rkey1", coll1)))

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "my-did"},
			"specific-reader",
		)
		require.NoError(t, err)
		require.Len(t, records, 1)
	})

	t.Run("permissions are scoped to collection", func(t *testing.T) {
		// reader-did has permission for coll1 but not coll2
		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll2, Repo: "my-did"},
			"reader-did",
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}
