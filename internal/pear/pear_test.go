package pear

import (
	"encoding/json"
	"fmt"
	"testing"

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
		require.NoError(
			t,
			perms.AddLexiconReadPermission(
				[]string{"reader-did"},
				"my-did",
				fmt.Sprintf("%s.*", coll1),
			),
		)

		records, err := p.listRecords(
			&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll1, Repo: "my-did"},
			"reader-did",
		)
		require.NoError(t, err)
		require.Len(t, records, 2)
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
