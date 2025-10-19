package privi

import (
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/eagraf/habitat-new/internal/permissions"
	"github.com/stretchr/testify/require"
)

// A unit test testing putRecord and getRecord with one basic permission.
// TODO: an integration test with two PDS's + privi servers running.
func TestControllerPrivateDataPutGet(t *testing.T) {
	// The val the caller is trying to put
	val := map[string]any{
		"someKey": "someVal",
	}
	marshalledVal, err := json.Marshal(val)
	require.NoError(t, err)

	dummy := permissions.NewDummyStore()
	db, err := sql.Open("sqlite3", ":memory:")
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

	marshalled, err := json.Marshal(got)
	require.NoError(t, err)
	require.Equal(t, []byte(marshalled), marshalledVal)

	err = p.putRecord("my-did", coll, val, rkey, &validate)
	require.NoError(t, err)
}
