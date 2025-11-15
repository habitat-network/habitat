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

	got, err := p.getRecord(coll, rkey, "my-did", "another-did")
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	require.NoError(t, dummy.AddLexiconReadPermission("another-did", "my-did", coll))

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
		"your-did",
	)
	require.NoError(t, err)
	require.Empty(t, records)

	require.NoError(
		t,
		dummy.AddLexiconReadPermission("your-did", "my-did", fmt.Sprintf("%s.*", coll)),
	)

	records, err = p.listRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: "my-did"},
		"your-did",
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}
