package pear

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
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

	repo, err := repo.NewRepo(db)
	require.NoError(t, err)
	inbox, err := inbox.New(db)
	require.NoError(t, err)
	p := NewPear(t.Context(), testServiceEndpoint, testServiceName, o.dir, xrpcchannel.NewDummy(), permissions, repo, inbox)
	return p
}

func mockIdentities(dids []string) identity.Directory {
	dir := identity.NewMockDirectory()
	for _, did := range dids {
		dir.Insert(identity.Identity{
			DID: syntax.DID(did),
			Services: map[string]identity.ServiceEndpoint{
				testServiceName: identity.ServiceEndpoint{
					URL: "https://" + testServiceEndpoint,
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
	require.Equal(t, id.Services[testServiceName].URL, "https://"+testServiceEndpoint)

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
	_, err := p.putRecord(t.Context(), "my-did", coll, val, rkey, &validate, nil)
	require.NoError(t, err)

	// Owner can always access their own records
	got, err := p.getRecord(t.Context(), coll, rkey, "my-did", "my-did")
	require.NoError(t, err)
	require.NotNil(t, got)

	var ownerUnmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &ownerUnmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, ownerUnmarshalled)

	// Non-owner without permission gets unauthorized
	got, err = p.getRecord(t.Context(), coll, rkey, "my-did", "another-did")
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	// Grant permission
	require.NoError(t, p.permissions.AddReadPermission([]string{"another-did"}, "my-did", coll))

	// Now non-owner can access
	got, err = p.getRecord(t.Context(), coll, "my-rkey", "my-did", "another-did")
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &unmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, unmarshalled)

	_, err = p.putRecord(t.Context(), "my-did", coll, val, rkey, &validate, nil)
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
	_, err := p.putRecord(t.Context(), "my-did", coll, val, rkey, &validate, nil)
	require.NoError(t, err)

	records, err := p.listRecords(t.Context(),
		"my-did",
		coll,
		"my-did",
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestGetRecordForwardingNotImplemented(t *testing.T) {
	dir := mockIdentities([]string{"did:plc:caller456"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// Try to get a record for a DID that doesn't exist on this server
	got, err := p.getRecord(t.Context(), "some.collection", "some-rkey", "did:plc:unknown123", "did:plc:caller456")
	require.Nil(t, got)
	require.ErrorIs(t, err, identity.ErrDIDNotFound)
}

func TestListRecordsForwardingNotImplemented(t *testing.T) {
	dir := mockIdentities([]string{"did:plc:caller456"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// Try to list records for a DID that doesn't exist on this server
	records, err := p.listRecords(t.Context(),
		"did:plc:unknown123",
		"some.collection",
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

	_, err := p.putRecord(t.Context(), "my-did", coll1, val, "rkey1", &validate, nil)
	require.NoError(t, err)
	_, err = p.putRecord(t.Context(), "my-did", coll1, val, "rkey2", &validate, nil)
	require.NoError(t, err)
	_, err = p.putRecord(t.Context(), "my-did", coll2, val, "rkey3", &validate, nil)
	require.NoError(t, err)

	t.Run("returns empty without permissions", func(t *testing.T) {
		records, err := p.listRecords(t.Context(),
			"my-did",
			coll1,
			"other-did",
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns records with wildcard permission", func(t *testing.T) {
		require.NoError(
			t,
			p.permissions.AddReadPermission(
				[]string{"reader-did"},
				"my-did",
				fmt.Sprintf("%s.*", coll1),
			),
		)

		records, err := p.listRecords(t.Context(),
			"my-did",
			coll1,
			"reader-did",
		)
		require.NoError(t, err)
		require.Len(t, records, 2)
	})

	t.Run("returns only specific permitted record", func(t *testing.T) {
		require.NoError(
			t,
			p.permissions.AddReadPermission(
				[]string{"specific-reader"},
				"my-did",
				fmt.Sprintf("%s.rkey1", coll1),
			),
		)

		records, err := p.listRecords(t.Context(),
			"my-did",
			coll1,
			"specific-reader",
		)
		require.NoError(t, err)
		require.Len(t, records, 1)
	})

	t.Run("permissions are scoped to collection", func(t *testing.T) {
		// reader-did has permission for coll1 but not coll2
		records, err := p.listRecords(t.Context(),
			"my-did",
			coll2,
			"reader-did",
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

func TestCliqueEndToEnd(t *testing.T) {
	ctx := t.Context()

	didA := "did:plc:aaaaaaaaaaaaaaaaaaaaaa"
	didB := "did:plc:bbbbbbbbbbbbbbbbbbbbbb"
	didC := "did:plc:cccccccccccccccccccccc"

	dir := mockIdentities([]string{didA, didB, didC})
	p := newPearForTest(t, withIdentityDirectory(dir))

	validate := true

	// Step 1: did A creates a record (this is the clique root).
	cliqueCollection := "network.habitat.clique"
	cliqueRkey := "clique-1"
	_, err := p.putRecord(ctx, didA, cliqueCollection, map[string]any{"name": "my-clique-root"}, cliqueRkey, &validate, nil)
	require.NoError(t, err)

	cliqueURI := habitat_syntax.ConstructHabitatUri(didA, cliqueCollection, cliqueRkey)

	// Step 2: did B creates a record and adds it to the clique originating at A's record.
	bCollection := "network.habitat.post"
	bRkey := "post-1"
	_, err = p.putRecord(ctx, didB, bCollection, map[string]any{"text": "hello from B"}, bRkey, &validate, nil)
	require.NoError(t, err)

	// B grants the clique read permission on B's record.
	err = p.addReadPermission(ctx, cliqueGrantee(cliqueURI), didB, bCollection, bRkey)
	require.NoError(t, err)

	// Step 3: did A can see that a notification about B's record exists in A's inbox.
	items, err := p.getCliqueItems(ctx, cliqueURI)
	require.NoError(t, err)
	require.NotEmpty(t, items, "A should see B's record in the clique")

	// Step 4: did A adds did C to the clique.
	err = p.addReadPermission(ctx, didGrantee(didC), didA, cliqueCollection, cliqueRkey)
	require.NoError(t, err)

	// Step 5: C should have received a notification for B's record as a result of A adding C.
	// Query C's inbox for notifications tagged with this clique.
	cliqueStr := string(cliqueURI)
	cInboxItems, err := p.inbox.GetCliqueItems(ctx, didC, cliqueStr)
	require.NoError(t, err)
	require.NotEmpty(t, cInboxItems, "C should have received a notification for B's record via the clique fan-out")
}

func TestNestedCliquesDisallowed(t *testing.T) {
	ctx := t.Context()

	didA := "did:plc:a"
	didB := "did:plc:b"
	didC := "did:plc:c"

	dir := mockIdentities([]string{didA, didB, didC})
	p := newPearForTest(t, withIdentityDirectory(dir))

	validate := true

	// A creates a clique root record.
	postCollection := "network.habitat.post"
	cliqueA, err := p.putRecord(ctx, didA, postCollection, map[string]any{"name": "clique-A"}, "clique-a", &validate, nil)
	require.NoError(t, err)

	// Now B creates a record and adds it to clique A.
	postRkey := "post-1"
	cliqueB, err := p.putRecord(ctx, didB, postCollection, map[string]any{"text": "hello"}, postRkey, &validate, []grantee{cliqueGrantee(cliqueA)})
	require.NoError(t, err)

	// Now C tries to create a record and add it to clique B.
	_, err = p.putRecord(ctx, didC, postCollection, map[string]any{"text": "hello"}, postRkey, &validate, []grantee{cliqueGrantee(cliqueB)})
	require.Error(t, err)
	require.Contains(t, err.Error(), "nested clique")
}

func TestPutRecordWithDidAndCliqueGrantees(t *testing.T) {
	ctx := t.Context()

	didA := "did:plc:aaaaaaaaaaaaaaaaaaaaaa"
	didB := "did:plc:bbbbbbbbbbbbbbbbbbbbbb"
	didC := "did:plc:cccccccccccccccccccccc"

	dir := mockIdentities([]string{didA, didB, didC})
	p := newPearForTest(t, withIdentityDirectory(dir))

	validate := true

	// A creates a clique root record.
	cliqueCollection := "network.habitat.clique"
	cliqueRkey := "clique-1"
	_, err := p.putRecord(ctx, didA, cliqueCollection, map[string]any{"name": "my-clique"}, cliqueRkey, &validate, nil)
	require.NoError(t, err)
	cliqueURI := habitat_syntax.ConstructHabitatUri(didA, cliqueCollection, cliqueRkey)

	// B creates a record and grants access to both did C (directly) and A's clique.
	bCollection := "network.habitat.post"
	bRkey := "post-1"
	uri, err := p.putRecord(ctx, didB, bCollection, map[string]any{"text": "shared post"}, bRkey, &validate, []grantee{
		didGrantee(didC),
		cliqueGrantee(cliqueURI),
	})
	require.NoError(t, err)
	require.NotEmpty(t, uri)

	// did C should have direct read permission on B's record.
	hasAccess, err := p.permissions.HasPermission(didC, didB, bCollection, bRkey)
	require.NoError(t, err)
	require.True(t, hasAccess, "C should have direct read permission on B's record")

	// The clique should also have been granted permission on B's record.
	hasAccess, err = p.permissions.HasPermission(string(cliqueURI), didB, bCollection, bRkey)
	require.NoError(t, err)
	require.True(t, hasAccess, "clique should have read permission on B's record")

	// A (the clique owner) should see B's record in the clique items.
	items, err := p.getCliqueItems(ctx, cliqueURI)
	require.NoError(t, err)
	require.NotEmpty(t, items, "A should see B's record via the clique")
}

// TODO: eventually test permissions with blobs here
func TestPearUploadAndGetBlob(t *testing.T) {
	dir := mockIdentities([]string{"did:example:alice"})
	pear := newPearForTest(t, withIdentityDirectory(dir))

	did := "did:example:alice"
	// use an empty blob to avoid hitting sqlite3.SQLITE_LIMIT_LENGTH in test environment
	blob := []byte("this is my test blob")
	mtype := "text/plain"

	bmeta, err := pear.uploadBlob(t.Context(), did, blob, mtype)
	require.NoError(t, err)
	require.NotNil(t, bmeta)
	require.Equal(t, mtype, bmeta.MimeType)
	require.Equal(t, int64(len(blob)), bmeta.Size)

	m, gotBlob, err := pear.getBlob(t.Context(), did, bmeta.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mtype, m)
	require.Equal(t, blob, gotBlob)
}
