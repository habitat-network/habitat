package pear

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const (
	testServiceName     = "habitat_test"
	testServiceEndpoint = "test_url"
)

type options struct {
	dir identity.Directory
}

type option func(*options)

func withIdentityDirectory(dir identity.Directory) option {
	return func(o *options) {
		o.dir = dir
	}
}

func newPearForTest(t *testing.T, opts ...option) *Pear {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	permissions, err := permissions.NewStore(db)
	require.NoError(t, err)

	o := &options{
		dir: identity.DefaultDirectory(),
	}
	for _, opt := range opts {
		opt(o)
	}
	fmt.Printf("%T\n", o.dir)

	repo, err := NewRepo(db)
	require.NoError(t, err)
	inbox, err := inbox.New(db)
	require.NoError(t, err)
	p := New(t.Context(), testServiceEndpoint, testServiceName, o.dir, permissions, repo, inbox)
	return p
}

func mockIdentities(dids []string) identity.Directory {
	dir := identity.NewMockDirectory()
	for _, did := range dids {
		dir.Insert(identity.Identity{
			DID: syntax.DID(did),
			Services: map[string]identity.ServiceEndpoint{
				testServiceName: identity.ServiceEndpoint{
					URL: testServiceEndpoint,
				},
			},
		})
	}
	return &dir
}

func TestMockIdentities(t *testing.T) {
	dir := mockIdentities([]string{"my-did", "another-did"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	id, err := dir.LookupDID(t.Context(), syntax.DID("my-did"))
	require.NoError(t, err)
	require.Equal(t, id.Services[testServiceName].URL, testServiceEndpoint)

	has, err := p.hasRepoForDid(syntax.DID("my-did"))
	require.NoError(t, err)
	require.True(t, has)
}

// A unit test testing putRecord and getRecord with one basic permission.
// TODO: an integration test with two PDS's + pear servers running.
func TestControllerPrivateDataPutGet(t *testing.T) {
	// The val the caller is trying to put
	val := map[string]any{
		"someKey": "someVal",
	}

	dir := mockIdentities([]string{"my-did", "another-did"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// putRecord
	coll := "my.fake.collection"
	rkey := "my-rkey"
	validate := true
	err := p.putRecord("my-did", coll, val, rkey, &validate)
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
	require.NoError(t, p.permissions.AddLexiconReadPermission([]string{"another-did"}, "my-did", coll))

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
	dir := mockIdentities([]string{"my-did"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// putRecord
	coll := "my.fake.collection"
	rkey := "my-rkey"
	validate := true
	err := p.putRecord("my-did", coll, val, rkey, &validate)
	require.NoError(t, err)

	records, err := p.listRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: coll, Repo: "my-did"},
		"my-did",
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestGetRecordForwardingNotImplemented(t *testing.T) {
	dir := mockIdentities([]string{"did:plc:caller456"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// Try to get a record for a DID that doesn't exist on this server
	got, err := p.getRecord("some.collection", "some-rkey", "did:plc:unknown123", "did:plc:caller456")
	require.Nil(t, got)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestListRecordsForwardingNotImplemented(t *testing.T) {
	dir := mockIdentities([]string{"did:plc:caller456"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// Try to list records for a DID that doesn't exist on this server
	records, err := p.listRecords(
		&habitat.NetworkHabitatRepoListRecordsParams{Collection: "some.collection", Repo: "did:plc:unknown123"},
		"did:plc:caller456",
	)
	require.Nil(t, records)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestListRecords(t *testing.T) {
	dir := mockIdentities([]string{"my-did", "other-did", "reader-did", "specific-reader"})
	p := newPearForTest(t, withIdentityDirectory(dir))

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
			p.permissions.AddLexiconReadPermission(
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
			p.permissions.AddLexiconReadPermission(
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
	dir := mockIdentities([]string{"did:example:alice"})
	pear := newPearForTest(t, withIdentityDirectory(dir))

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
