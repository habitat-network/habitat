package pear

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type mockXrpcChannel struct {
	actions []*http.Response
}

func (m *mockXrpcChannel) SendXRPC(_ context.Context, _ syntax.DID, _ syntax.DID, r *http.Request) (*http.Response, error) {
	next := m.actions[0]
	m.actions = m.actions[1:]
	return next, nil
}

var _ xrpcchannel.XrpcChannel = &mockXrpcChannel{}

const (
	testServiceName     = "habitat_test"
	testServiceEndpoint = "https://test_url"
)

type options struct {
	node node.Node
}

type option func(*options)

func withNode(node node.Node) option {
	return func(o *options) {
		o.node = node
	}
}

func newPearForTest(t *testing.T, dir identity.Directory, opts ...option) *pear {
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	require.NoError(t, err)

	o := &options{
		node: node.New(testServiceName, testServiceEndpoint, dir, &mockXrpcChannel{}),
	}
	for _, opt := range opts {
		opt(o)
	}

	repo, err := repo.NewRepo(db)
	require.NoError(t, err)
	inbox, err := inbox.New(db)
	require.NoError(t, err)

	permissions, err := permissions.NewStore(db, o.node)
	require.NoError(t, err)
	p := NewPear(o.node, dir, permissions, repo, inbox)
	return p
}

func mockIdentities(dids []syntax.DID) identity.Directory {
	dir := identity.NewMockDirectory()
	for _, did := range dids {
		dir.Insert(identity.Identity{
			DID: did,
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
	example := syntax.DID("did:example:myid")
	dir := mockIdentities([]syntax.DID{example})
	p := newPearForTest(t, dir)

	id, err := dir.LookupDID(t.Context(), example)
	require.NoError(t, err)
	require.Equal(t, id.Services[testServiceName].URL, testServiceEndpoint)

	has, err := p.node.ServesDID(t.Context(), example)
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

	dir := mockIdentities([]syntax.DID{"did:example:myid", "did:example:anotherid"})
	p := newPearForTest(t, dir)

	// putRecord
	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true
	uri, err := p.PutRecord(t.Context(), syntax.DID("did:example:myid"), syntax.DID("did:example:myid"), coll, val, rkey, &validate, []permissions.Grantee{})
	require.NoError(t, err)
	require.Equal(t, habitat_syntax.ConstructHabitatUri("did:example:myid", coll.String(), rkey.String()), uri)

	// Owner can always access their own records
	got, err := p.GetRecord(t.Context(), coll, rkey, syntax.DID("did:example:myid"), syntax.DID("did:example:myid"))
	require.NoError(t, err)
	require.NotNil(t, got)

	var ownerUnmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &ownerUnmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, ownerUnmarshalled)

	// Non-owner without permission gets unauthorized
	got, err = p.GetRecord(t.Context(), coll, rkey, syntax.DID("did:example:myid"), syntax.DID("did:example:anotherid"))
	require.Nil(t, got)
	require.ErrorIs(t, ErrUnauthorized, err)

	// Grant permission
	require.NoError(t, p.permissions.AddPermissions([]permissions.Grantee{permissions.DIDGrantee("did:example:anotherid")}, syntax.DID("did:example:myid"), coll, ""))

	// Now non-owner can access
	got, err = p.GetRecord(t.Context(), coll, rkey, syntax.DID("did:example:myid"), syntax.DID("did:example:anotherid"))
	require.NoError(t, err)

	var unmarshalled map[string]any
	err = json.Unmarshal([]byte(got.Value), &unmarshalled)
	require.NoError(t, err)
	require.Equal(t, val, unmarshalled)

	_, err = p.PutRecord(t.Context(), syntax.DID("did:example:myid"), syntax.DID("did:example:myid"), coll, val, rkey, &validate, []permissions.Grantee{})
	require.NoError(t, err)
}

func TestListOwnRecords(t *testing.T) {
	val := map[string]any{
		"someKey": "someVal",
	}
	dir := mockIdentities([]syntax.DID{"did:example:myid"})
	p := newPearForTest(t, dir)

	// putRecord
	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true
	uri, err := p.PutRecord(t.Context(), syntax.DID("did:example:myid"), syntax.DID("did:example:myid"), coll, val, rkey, &validate, []permissions.Grantee{})
	require.NoError(t, err)
	require.Equal(t, habitat_syntax.ConstructHabitatUri("did:example:myid", coll.String(), rkey.String()), uri)

	records, err := p.ListRecords(
		t.Context(),
		syntax.DID("did:example:myid"),
		coll,
	)
	require.NoError(t, err)
	require.Len(t, records, 1)
}

func TestListRecords(t *testing.T) {
	dir := mockIdentities([]syntax.DID{"did:example:myid", "did:example:otherid", "did:example:readerid", "did:example:specificreader"})
	p := newPearForTest(t, dir)

	val := map[string]any{"someKey": "someVal"}
	validate := true

	// Create multiple records across collections
	coll1 := syntax.NSID("my.fake.collection1")
	coll2 := syntax.NSID("my.fake.collection2")

	_, err := p.PutRecord(t.Context(), syntax.DID("did:example:myid"), syntax.DID("did:example:myid"), coll1, val, "rkey1", &validate, []permissions.Grantee{})
	require.NoError(t, err)
	_, err = p.PutRecord(t.Context(), syntax.DID("did:example:myid"), syntax.DID("did:example:myid"), coll1, val, "rkey2", &validate, []permissions.Grantee{})
	require.NoError(t, err)
	_, err = p.PutRecord(t.Context(), syntax.DID("did:example:myid"), syntax.DID("did:example:myid"), coll2, val, "rkey3", &validate, []permissions.Grantee{})
	require.NoError(t, err)

	t.Run("returns empty without permissions", func(t *testing.T) {
		records, err := p.ListRecords(
			t.Context(),
			syntax.DID("did:example:otherid"),
			coll1,
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})

	t.Run("returns records with full collection permission", func(t *testing.T) {
		require.NoError(
			t,
			p.permissions.AddPermissions(
				[]permissions.Grantee{permissions.DIDGrantee("did:example:readerid")},
				syntax.DID("did:example:myid"),
				coll1,
				"",
			),
		)

		records, err := p.ListRecords(
			t.Context(),
			syntax.DID("did:example:readerid"),
			coll1,
		)
		require.NoError(t, err)
		// did:example:readerid has permission to see all did:example:myid's records in coll1
		require.Len(t, records, 2)
		require.Equal(t, "did:example:myid", records[0].Did)
		require.Equal(t, "did:example:myid", records[1].Did)
	})

	t.Run("returns only specific permitted record", func(t *testing.T) {
		require.NoError(
			t,
			p.permissions.AddPermissions(
				[]permissions.Grantee{permissions.DIDGrantee("did:example:specificreader")},
				syntax.DID("did:example:myid"),
				coll1,
				"rkey1",
			),
		)

		records, err := p.ListRecords(
			t.Context(),
			syntax.DID("did:example:specificreader"),
			coll1,
		)
		require.NoError(t, err)
		// did:example:specificreader has permission only for rkey1
		require.Len(t, records, 1)
		require.Equal(t, "did:example:myid", records[0].Did)
		require.Equal(t, "rkey1", records[0].Rkey)
	})

	t.Run("permissions are scoped to collection", func(t *testing.T) {
		// did:example:readerid has permission for coll1 but not coll2
		records, err := p.ListRecords(
			t.Context(),
			syntax.DID("did:example:readerid"),
			coll2,
		)
		require.NoError(t, err)
		require.Empty(t, records)
	})
}

// TestPutRecordWithGrantees verifies that grantees passed to putRecord gain read access.
func TestPutRecordWithGrantees(t *testing.T) {
	ownerDID := syntax.DID("did:plc:owner")
	grantee1DID := syntax.DID("did:plc:grantee1")
	grantee2DID := syntax.DID("did:plc:grantee2")
	nonGranteeDID := syntax.DID("did:plc:nongrantee")

	dir := mockIdentities([]syntax.DID{ownerDID, grantee1DID, grantee2DID, nonGranteeDID})
	p := newPearForTest(t, dir)

	val := map[string]any{"data": "secret"}
	coll := syntax.NSID("my.fake.collection")
	rkey := syntax.RecordKey("my-rkey")
	validate := true

	// Put the record and grant access to grantee1 and grantee2 at the same time.
	uri, err := p.PutRecord(t.Context(), syntax.DID(ownerDID), syntax.DID(ownerDID), coll, val, rkey, &validate, []permissions.Grantee{permissions.DIDGrantee(syntax.DID(grantee1DID)), permissions.DIDGrantee(syntax.DID(grantee2DID))})
	require.NoError(t, err)
	require.Equal(t, habitat_syntax.ConstructHabitatUri(ownerDID.String(), coll.String(), rkey.String()), uri)

	// Grantees can read the record.
	for _, grantee := range []string{grantee1DID.String(), grantee2DID.String()} {
		got, err := p.GetRecord(t.Context(), coll, rkey, syntax.DID(ownerDID), syntax.DID(grantee))
		require.NoError(t, err, "grantee %s should be able to read the record", grantee)
		require.NotNil(t, got)

		var unmarshalled map[string]any
		require.NoError(t, json.Unmarshal([]byte(got.Value), &unmarshalled))
		require.Equal(t, val, unmarshalled)
	}

	// A non-grantee cannot read the record.
	got, err := p.GetRecord(t.Context(), coll, rkey, syntax.DID(ownerDID), syntax.DID(nonGranteeDID))
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrUnauthorized)
}

// TestPutRecordCrossUserUnauthorized verifies that a caller cannot put a record
// into another user's repo (i.e. callerDID != targetDID is rejected).
func TestPutRecordCrossUserUnauthorized(t *testing.T) {
	callerDID := syntax.DID("did:plc:caller")
	targetDID := syntax.DID("did:plc:target")

	dir := mockIdentities([]syntax.DID{callerDID, targetDID})
	p := newPearForTest(t, dir)

	val := map[string]any{"data": "value"}
	validate := true

	_, err := p.PutRecord(t.Context(), syntax.DID(callerDID), syntax.DID(targetDID), "my.fake.collection", val, "some-rkey", &validate, []permissions.Grantee{})
	require.Error(t, err)
}

func TestCliqueFlow(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	dir := mockIdentities([]syntax.DID{aDID, bDID, cDID})
	p := newPearForTest(t, dir)

	cliqueRkey := syntax.RecordKey("shared-clique")
	clique := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), cliqueRkey.String()))

	// A creates the clique by adding B as a member
	require.NoError(t, p.permissions.AddPermissions(
		[]permissions.Grantee{permissions.DIDGrantee(syntax.DID(bDID))},
		syntax.DID(aDID),
		permissions.CliqueNSID,
		cliqueRkey,
	))

	val := map[string]any{"data": "value"}
	validate := true
	coll := syntax.NSID("my.fake.collection")
	aRkey := syntax.RecordKey("a-record")
	bRkey := syntax.RecordKey("b-record")

	// A and B both are direct grantees of the clique
	bauthz, err := p.HasPermission(t.Context(), syntax.DID(bDID), syntax.DID(aDID), permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)
	require.True(t, bauthz)

	aauthz, err := p.HasPermission(t.Context(), syntax.DID(bDID), syntax.DID(aDID), permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)
	require.True(t, aauthz)

	// A creates a record and grants access to the clique
	_, err = p.PutRecord(t.Context(), syntax.DID(aDID), syntax.DID(aDID), coll, val, aRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)

	// B creates a record and grants access to the same clique
	_, err = p.PutRecord(t.Context(), syntax.DID(bDID), syntax.DID(bDID), coll, val, bRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)

	// Both A and B can see both records
	got, err := p.GetRecord(t.Context(), coll, aRkey, syntax.DID(aDID), syntax.DID(aDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	got, err = p.GetRecord(t.Context(), coll, bRkey, syntax.DID(bDID), syntax.DID(aDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	got, err = p.GetRecord(t.Context(), coll, bRkey, syntax.DID(bDID), syntax.DID(bDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	got, err = p.GetRecord(t.Context(), coll, aRkey, syntax.DID(aDID), syntax.DID(bDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	// Both A and B can list both records via ListRecords
	aRecords, err := p.ListRecords(t.Context(), syntax.DID(aDID), coll)
	require.NoError(t, err)
	require.Len(t, aRecords, 2)

	bRecords, err := p.ListRecords(t.Context(), syntax.DID(bDID), coll)
	require.NoError(t, err)
	require.Len(t, bRecords, 2)

	// A adds C to the clique
	require.NoError(t, p.permissions.AddPermissions(
		[]permissions.Grantee{permissions.DIDGrantee(syntax.DID(cDID))},
		syntax.DID(aDID),
		permissions.CliqueNSID,
		cliqueRkey,
	))

	// C can see both records
	got, err = p.GetRecord(t.Context(), coll, aRkey, syntax.DID(aDID), syntax.DID(cDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	got, err = p.GetRecord(t.Context(), coll, bRkey, syntax.DID(bDID), syntax.DID(cDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	// C can also list both records via ListRecords
	cRecords, err := p.ListRecords(t.Context(), syntax.DID(cDID), coll)
	require.NoError(t, err)
	require.Len(t, cRecords, 2)

	// A removes B from the clique
	require.NoError(t, p.permissions.RemovePermissions(
		[]permissions.Grantee{permissions.DIDGrantee(syntax.DID(bDID))},
		syntax.DID(aDID),
		permissions.CliqueNSID,
		cliqueRkey,
	))

	// B can no longer see A's record
	got, err = p.GetRecord(t.Context(), coll, aRkey, syntax.DID(aDID), syntax.DID(bDID))
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrUnauthorized)

	// B can still see its own record
	got, err = p.GetRecord(t.Context(), coll, bRkey, syntax.DID(bDID), syntax.DID(bDID))
	require.NoError(t, err)
	require.NotNil(t, got)

	// B can no longer list A's record; only sees its own
	bRecordsAfterRemoval, err := p.ListRecords(t.Context(), syntax.DID(bDID), coll)
	require.NoError(t, err)
	require.Len(t, bRecordsAfterRemoval, 1)
	require.Equal(t, bDID.String(), bRecordsAfterRemoval[0].Did)
}

func TestNestedCliquesProhibited(t *testing.T) {
	ownerDID := syntax.DID("did:example:cliqueowner")
	otherOwnerDID := syntax.DID("did:example:otherowner")

	dir := mockIdentities([]syntax.DID{ownerDID, otherOwnerDID})
	p := newPearForTest(t, dir)

	val := map[string]any{"members": []string{}}
	validate := true

	// Granting a CliqueGrantee access to a clique record should be rejected
	nestedClique := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(otherOwnerDID.String(), permissions.CliqueNSID.String(), "other-clique"))
	_, err := p.PutRecord(
		t.Context(),
		syntax.DID(ownerDID),
		syntax.DID(ownerDID),
		permissions.CliqueNSID,
		val,
		"my-clique",
		&validate,
		[]permissions.Grantee{nestedClique},
	)
	require.ErrorIs(t, err, ErrNoNestedCliques)
}

func TestNotifyOfUpdate(t *testing.T) {
	senderDID := syntax.DID("did:plc:sender")
	recipientDID := syntax.DID("did:plc:recipient")

	dir := mockIdentities([]syntax.DID{senderDID, recipientDID})
	p := newPearForTest(t, dir)

	collection := "my.fake.collection"
	rkey := "my-rkey"

	t.Run("succeeds for valid sender and recipient", func(t *testing.T) {
		err := p.NotifyOfUpdate(
			t.Context(),
			syntax.DID(senderDID),
			syntax.DID(recipientDID),
			collection,
			rkey,
		)
		require.NoError(t, err)
	})

	t.Run("is idempotent: same notification twice does not error", func(t *testing.T) {
		err := p.NotifyOfUpdate(
			t.Context(),
			syntax.DID(senderDID),
			syntax.DID(recipientDID),
			collection,
			rkey,
		)
		require.NoError(t, err)

		err = p.NotifyOfUpdate(
			t.Context(),
			syntax.DID(senderDID),
			syntax.DID(recipientDID),
			collection,
			rkey,
		)
		require.NoError(t, err)
	})

	t.Run("different rkeys each succeed", func(t *testing.T) {
		err := p.NotifyOfUpdate(
			t.Context(),
			syntax.DID(senderDID),
			syntax.DID(recipientDID),
			collection,
			"rkey-1",
		)
		require.NoError(t, err)

		err = p.NotifyOfUpdate(
			t.Context(),
			syntax.DID(senderDID),
			syntax.DID(recipientDID),
			collection,
			"rkey-2",
		)
		require.NoError(t, err)
	})
}

// TODO: eventually test permissions with blobs here
func TestPearUploadAndGetBlob(t *testing.T) {
	dir := mockIdentities([]syntax.DID{"did:example:alice"})
	pear := newPearForTest(t, dir)

	did := "did:example:alice"
	// use an empty blob to avoid hitting sqlite3.SQLITE_LIMIT_LENGTH in test environment
	blob := []byte("this is my test blob")
	mtype := "text/plain"

	bmeta, err := pear.UploadBlob(t.Context(), did, blob, mtype)
	require.NoError(t, err)
	require.NotNil(t, bmeta)
	require.Equal(t, mtype, bmeta.MimeType)
	require.Equal(t, int64(len(blob)), bmeta.Size)

	m, gotBlob, err := pear.GetBlob(t.Context(), did, bmeta.Ref.String())
	require.NoError(t, err)
	require.Equal(t, mtype, m)
	require.Equal(t, blob, gotBlob)
}

func TestListRecordsWithPermissions(t *testing.T) {
	// Note: this test doesn't include any remote users. Querying from remote as well isn't supported yet.
	// Set up users
	aliceDID := syntax.DID("did:plc:alice")
	bobDID := syntax.DID("did:plc:bob")
	carolDID := syntax.DID("did:plc:carol")
	remoteDID := syntax.DID("did:plc:remote")

	// Create a shared database for the test
	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)

	// Create pear with the shared database
	repoStore, err := repo.NewRepo(db)
	require.NoError(t, err)
	inboxInstance, err := inbox.New(db)
	require.NoError(t, err)
	// remoteDID is intentionally not added to mock identities to simulate a different node
	dir := mockIdentities([]syntax.DID{aliceDID, bobDID, carolDID})
	n := node.New(testServiceName, testServiceEndpoint, dir, &mockXrpcChannel{})

	perms, err := permissions.NewStore(db, n)
	require.NoError(t, err)
	p := NewPear(n, dir, perms, repoStore, inboxInstance)

	val := map[string]any{"someKey": "someVal"}
	validate := true
	coll := syntax.NSID("my.fake.collection")

	// Alice creates her own records
	_, err = p.PutRecord(t.Context(), syntax.DID(aliceDID), syntax.DID(aliceDID), coll, val, "alice-rkey1", &validate, []permissions.Grantee{})
	require.NoError(t, err)
	_, err = p.PutRecord(t.Context(), syntax.DID(aliceDID), syntax.DID(aliceDID), coll, val, "alice-rkey2", &validate, []permissions.Grantee{})
	require.NoError(t, err)

	// Bob creates records
	_, err = p.PutRecord(t.Context(), syntax.DID(bobDID), syntax.DID(bobDID), coll, val, "bob-rkey1", &validate, []permissions.Grantee{})
	require.NoError(t, err)
	_, err = p.PutRecord(t.Context(), syntax.DID(bobDID), syntax.DID(bobDID), coll, val, "bob-rkey2", &validate, []permissions.Grantee{})
	require.NoError(t, err)

	// Carol creates records
	_, err = p.PutRecord(t.Context(), syntax.DID(carolDID), syntax.DID(carolDID), coll, val, "carol-rkey1", &validate, []permissions.Grantee{})
	require.NoError(t, err)

	t.Run("includes records from other users when user has permission", func(t *testing.T) {
		// Grant Alice permission to read Bob's records
		require.NoError(t, perms.AddPermissions([]permissions.Grantee{permissions.DIDGrantee(syntax.DID(aliceDID))}, syntax.DID(bobDID), coll, ""))

		records, err := p.ListRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
		)
		require.NoError(t, err)
		require.Len(t, records, 4) // 2 from Alice's own repo + 2 from Bob with permission

		// Verify we have Alice's and Bob's records
		aliceRecords := 0
		bobRecords := 0
		for _, record := range records {
			switch syntax.DID(record.Did) {
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
		records, err := p.ListRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
		)
		require.NoError(t, err)
		// Should be 4 (2 from Alice + 2 from Bob with permission, but NOT Carol's)
		require.Len(t, records, 4)

		// Verify Carol's record is not included
		for _, record := range records {
			require.NotEqual(
				t,
				carolDID,
				record.Did,
				"Carol's record should not be included without permission",
			)
		}
	})

	t.Run("includes records from different nodes if they exist in database", func(t *testing.T) {
		// Grant Alice permission to read remote user's records
		require.NoError(t, perms.AddPermissions([]permissions.Grantee{permissions.DIDGrantee(syntax.DID(aliceDID))}, syntax.DID(remoteDID), coll, ""))

		records, err := p.ListRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
		)
		require.NoError(t, err)
		require.Len(t, records, 4)
	})

	t.Run("filters by collection", func(t *testing.T) {
		otherColl := syntax.NSID("other.collection")
		_, err := p.PutRecord(t.Context(), syntax.DID(bobDID), syntax.DID(bobDID), otherColl, val, "bob-other-rkey", &validate, []permissions.Grantee{})
		require.NoError(t, err)
		require.NoError(t, perms.AddPermissions([]permissions.Grantee{permissions.DIDGrantee(syntax.DID(aliceDID))}, syntax.DID(bobDID), otherColl, ""))

		// Query for original collection
		records, err := p.ListRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
		)
		require.NoError(t, err)
		require.Len(t, records, 4)

		// Query for other collection
		records, err = p.ListRecords(
			t.Context(),
			syntax.DID(aliceDID),
			otherColl,
		)
		require.NoError(t, err)
		// Should have 1 record from Bob (Alice doesn't have own records in otherColl)
		require.Len(t, records, 1)
		require.Equal(t, bobDID.String(), records[0].Did)
		require.Equal(t, "bob-other-rkey", records[0].Rkey)
	})

	t.Run("returns only specific permitted records", func(t *testing.T) {
		// Remove full collection permission first, then grant only specific permission
		require.NoError(t, perms.RemovePermissions([]permissions.Grantee{permissions.DIDGrantee(syntax.DID(aliceDID))}, syntax.DID(bobDID), coll, ""))
		require.NoError(
			t,
			perms.AddPermissions([]permissions.Grantee{permissions.DIDGrantee(syntax.DID(aliceDID))}, syntax.DID(bobDID), coll, "bob-rkey1"),
		)

		records, err := p.ListRecords(
			t.Context(),
			syntax.DID(aliceDID),
			coll,
		)
		require.NoError(t, err)
		// Should have at least 2 from Alice + 1 specific from Bob (may include remote if it exists)
		require.GreaterOrEqual(t, len(records), 3)

		// Verify we have the right records from Bob
		bobRkey1Found := false
		for _, record := range records {
			if record.Did == bobDID.String() && record.Rkey == "bob-rkey1" {
				bobRkey1Found = true
			}
			if record.Did == bobDID.String() {
				require.Equal(
					t,
					"bob-rkey1",
					record.Rkey,
					"Should only have bob-rkey1, not bob-rkey2",
				)
			}
		}
		require.True(t, bobRkey1Found, "Should have found bob-rkey1")
	})
}
