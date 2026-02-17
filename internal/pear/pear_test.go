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
	p := NewPear(t.Context(), testServiceEndpoint, testServiceName, o.dir, permissions, repo, inbox)
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
	uri, err := p.putRecord(t.Context(), "my-did", coll, val, rkey, &validate)
	require.NoError(t, err)
	require.Equal(t, habitat_syntax.ConstructHabitatUri("my-did", coll, rkey), uri)

	// Owner can always access their own records
	got, err := p.getRecord(t.Context(), coll, rkey, syntax.DID("my-did"), syntax.DID("my-did"))
	require.NoError(t, err)
	require.NotNil(t, got)

	var ownerUnmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &ownerUnmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, ownerUnmarshalled)

	// Non-owner without permission gets unauthorized
	got, err = p.getRecord(t.Context(), coll, rkey, syntax.DID("my-did"), syntax.DID("another-did"))
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	// Grant permission
	require.NoError(t, p.permissions.AddReadPermission([]string{"another-did"}, "my-did", coll))

	// Now non-owner can access
	got, err = p.getRecord(t.Context(), coll, "my-rkey", syntax.DID("my-did"), syntax.DID("another-did"))
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &unmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, unmarshalled)

	_, err = p.putRecord(t.Context(), "my-did", coll, val, rkey, &validate)
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
	uri, err := p.putRecord(t.Context(), "my-did", coll, val, rkey, &validate)
	require.NoError(t, err)
	require.Equal(t, habitat_syntax.ConstructHabitatUri("my-did", coll, rkey), uri)

	records, err := p.listRecords(
		t.Context(),
		syntax.DID("my-did"),
		coll,
		syntax.DID("my-did"),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestGetRecordForwardingNotImplemented(t *testing.T) {
	dir := mockIdentities([]string{"did:plc:caller456"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// Try to get a record for a DID that doesn't exist on this server
	// This will return ErrUnauthorized since we no longer check for local repos
	got, err := p.getRecord(t.Context(), "some.collection", "some-rkey", syntax.DID("did:plc:unknown123"), syntax.DID("did:plc:caller456"))
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestListRecordsForwardingNotImplemented(t *testing.T) {
	dir := mockIdentities([]string{"did:plc:caller456"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	// Try to list records for a DID that doesn't exist on this server
	// This will return empty results since we no longer check for local repos
	records, err := p.listRecords(
		t.Context(),
		syntax.DID("did:plc:unknown123"),
		"some.collection",
		syntax.DID("did:plc:caller456"),
	)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestListRecords(t *testing.T) {
	dir := mockIdentities([]string{"my-did", "other-did", "reader-did", "specific-reader"})
	p := newPearForTest(t, withIdentityDirectory(dir))

	val := map[string]any{"someKey": "someVal"}
	validate := true

	// Create multiple records across collections
	coll1 := "my.fake.collection1"
	coll2 := "my.fake.collection2"

	_, err := p.putRecord(t.Context(), "my-did", coll1, val, "rkey1", &validate)
	require.NoError(t, err)
	_, err = p.putRecord(t.Context(), "my-did", coll1, val, "rkey2", &validate)
	require.NoError(t, err)
	_, err = p.putRecord(t.Context(), "my-did", coll2, val, "rkey3", &validate)
	require.NoError(t, err)

	t.Run("returns empty without permissions", func(t *testing.T) {
		records, err := p.listRecords(
			t.Context(),
			syntax.DID("my-did"),
			coll1,
			syntax.DID("other-did"),
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns records with full collection permission", func(t *testing.T) {
		require.NoError(
			t,
			p.permissions.AddReadPermission(
				[]string{"reader-did"},
				"my-did",
				coll1,
			),
		)

		records, err := p.listRecords(
			t.Context(),
			syntax.DID("my-did"),
			coll1,
			syntax.DID("reader-did"),
		)
		require.NoError(t, err)
		// reader-did has permission to see all my-did's records in coll1
		require.Len(t, records, 2)
		require.Equal(t, "my-did", records[0].Did)
		require.Equal(t, "my-did", records[1].Did)
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

		records, err := p.listRecords(
			t.Context(),
			syntax.DID("my-did"),
			coll1,
			syntax.DID("specific-reader"),
		)
		require.NoError(t, err)
		// specific-reader has permission only for rkey1
		require.Len(t, records, 1)
		require.Equal(t, "my-did", records[0].Did)
		require.Equal(t, "rkey1", records[0].Rkey)
	})

	t.Run("permissions are scoped to collection", func(t *testing.T) {
		// reader-did has permission for coll1 but not coll2
		records, err := p.listRecords(
			t.Context(),
			syntax.DID("my-did"),
			coll2,
			syntax.DID("reader-did"),
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

func TestListRecordsWithPermissions(t *testing.T) {

	// Note: this test doesn't include any remote users. Querying from remote as well isn't supported yet.
	// Set up users
	aliceDID := "did:plc:alice"
	bobDID := "did:plc:bob"
	carolDID := "did:plc:carol"
	remoteDID := "did:plc:remote"

	// Create a shared database for the test
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)

	// Create pear with the shared database
	perms, err := permissions.NewStore(db)
	require.NoError(t, err)
	repoStore, err := repo.NewRepo(db)
	require.NoError(t, err)
	inboxInstance, err := inbox.New(db)
	require.NoError(t, err)
	// remoteDID is intentionally not added to mock identities to simulate a different node
	p := NewPear(t.Context(), testServiceEndpoint, testServiceName, mockIdentities([]string{aliceDID, bobDID, carolDID}), perms, repoStore, inboxInstance)

	val := map[string]any{"someKey": "someVal"}
	validate := true
	coll := "my.fake.collection"

	// Alice creates her own records
	_, err = p.putRecord(t.Context(), aliceDID, coll, val, "alice-rkey1", &validate)
	require.NoError(t, err)
	_, err = p.putRecord(t.Context(), aliceDID, coll, val, "alice-rkey2", &validate)
	require.NoError(t, err)

	// Bob creates records
	_, err = p.putRecord(t.Context(), bobDID, coll, val, "bob-rkey1", &validate)
	require.NoError(t, err)
	_, err = p.putRecord(t.Context(), bobDID, coll, val, "bob-rkey2", &validate)
	require.NoError(t, err)

	// Carol creates records
	_, err = p.putRecord(t.Context(), carolDID, coll, val, "carol-rkey1", &validate)
	require.NoError(t, err)

	t.Run("includes records from other users when user has permission", func(t *testing.T) {
		// Grant Alice permission to read Bob's records
		require.NoError(t, perms.AddReadPermission([]string{aliceDID}, bobDID, coll))

		records, err := p.listRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		require.Len(t, records, 4) // 2 from Alice's own repo + 2 from Bob with permission

		// Verify we have Alice's and Bob's records
		aliceRecords := 0
		bobRecords := 0
		for _, record := range records {
			switch record.Did {
			case aliceDID:
				aliceRecords++
			case bobDID:
				bobRecords++
			default:
				require.Fail(t, "unexpected record did: %s", record.Did)
			}
		}
		require.Equal(t, 2, aliceRecords)
		require.Equal(t, 2, bobRecords)
	})

	t.Run("excludes records when user lacks permission", func(t *testing.T) {
		// Alice doesn't have permission for Carol's records
		records, err := p.listRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should be 4 (2 from Alice + 2 from Bob with permission, but NOT Carol's)
		require.Len(t, records, 4)

		// Verify Carol's record is not included
		for _, record := range records {
			require.NotEqual(t, carolDID, record.Did, "Carol's record should not be included without permission")
		}
	})

	t.Run("includes records from different nodes if they exist in database", func(t *testing.T) {
		// Grant Alice permission to read remote user's records
		require.NoError(t, perms.AddReadPermission([]string{aliceDID}, remoteDID, coll))

		records, err := p.listRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		require.Len(t, records, 4)
	})

	t.Run("filters by collection", func(t *testing.T) {
		otherColl := "other.collection"
		_, err := p.putRecord(t.Context(), bobDID, otherColl, val, "bob-other-rkey", &validate)
		require.NoError(t, err)
		require.NoError(t, perms.AddReadPermission([]string{aliceDID}, bobDID, otherColl))

		// Query for original collection
		records, err := p.listRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		require.Len(t, records, 4)

		// Query for other collection
		records, err = p.listRecords(
			t.Context(),
			syntax.DID(aliceDID),
			otherColl,
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should have 1 record from Bob (Alice doesn't have own records in otherColl)
		require.Len(t, records, 1)
		require.Equal(t, bobDID, records[0].Did)
		require.Equal(t, "bob-other-rkey", records[0].Rkey)
	})

	t.Run("returns only specific permitted records", func(t *testing.T) {
		// Remove full collection permission first, then grant only specific permission
		require.NoError(t, perms.RemoveReadPermission(aliceDID, bobDID, coll))
		require.NoError(t, perms.AddReadPermission([]string{aliceDID}, bobDID, fmt.Sprintf("%s.bob-rkey1", coll)))

		records, err := p.listRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
			syntax.DID(aliceDID),
		)
		require.NoError(t, err)
		// Should have at least 2 from Alice + 1 specific from Bob (may include remote if it exists)
		require.GreaterOrEqual(t, len(records), 3)

		// Verify we have the right records from Bob
		bobRkey1Found := false
		for _, record := range records {
			if record.Did == bobDID && record.Rkey == "bob-rkey1" {
				bobRkey1Found = true
			}
			if record.Did == bobDID {
				require.Equal(t, "bob-rkey1", record.Rkey, "Should only have bob-rkey1, not bob-rkey2")
			}
		}
		require.True(t, bobRkey1Found, "Should have found bob-rkey1")
	})
}
