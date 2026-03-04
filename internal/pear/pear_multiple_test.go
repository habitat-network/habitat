package pear

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/permissions"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
	"github.com/stretchr/testify/require"
)

func newMultiPears(t *testing.T, mockXrpcCh xrpcchannel.XrpcChannel, dids ...[]syntax.DID) []*pear {
	pears := make([]*pear, len(dids))
	for i, didGroup := range dids {
		name := fmt.Sprintf("pear%c", 'A'+i)
		endpoint := fmt.Sprintf("https://pear%c", 'A'+i)

		dir := identity.NewMockDirectory()
		for _, did := range didGroup {
			dir.Insert(identity.Identity{
				DID: did,
				Services: map[string]identity.ServiceEndpoint{
					name: {URL: endpoint},
				},
			})
		}

		n := node.New(name, endpoint, dir, mockXrpcCh)
		pears[i] = newPearForTest(t, dir, withNode(n))
	}
	return pears
}

func TestCliqueFlowMultiPear(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	mockXRPCs := newMockXrpcChannel(t)
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID, cDID}, []syntax.DID{bDID})
	pearAC, pearB := ps[0], ps[1]

	cliqueRkey := syntax.RecordKey("shared-clique")
	clique := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), cliqueRkey.String()))
	cliqueMembers := []permissions.Grantee{permissions.DIDGrantee(bDID)}

	// A creates the clique by adding B as a member
	_, err := pearAC.permissions.AddPermissions(
		cliqueMembers,
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	)
	require.NoError(t, err)

	val := map[string]any{"data": "value"}
	validate := true
	coll := syntax.NSID("my.fake.collection")
	aRkey := syntax.RecordKey("a-record")
	bRkey := syntax.RecordKey("b-record")

	// A and B both are direct grantees of the clique
	bauthz, err := pearAC.permissions.HasPermission(t.Context(), bDID, aDID, permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)
	require.True(t, bauthz)

	aauthz, err := pearAC.permissions.HasPermission(t.Context(), bDID, aDID, permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)
	require.True(t, aauthz)

	// Pre-compute the clique response body (reused for multiple XRPC calls that resolve the clique).
	cliqueBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:         fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), permissions.CliqueNSID, cliqueRkey),
		Value:       map[string]any{},
		Permissions: permissions.ConstructInterfaceFromGrantees(cliqueMembers),
	})
	require.NoError(t, err)

	// A creates a record and grants access to the clique.
	// PutRecord resolves the clique (local, no XRPC) then calls NotifyOfUpdate to B's node (XRPC 1).
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	// Manually deliver notification to B's inbox (simulating B's node receiving the XRPC call).
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, aRkey, clique.String())
	require.NoError(t, err)
	_, err = pearAC.PutRecord(t.Context(), aDID, aDID, coll, val, aRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// B creates a record and grants access to the clique.
	// PutRecord resolves the clique from A's node (getRecord XRPC 2) then notifies A (notifyOfUpdate XRPC 3).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(cliqueBodyBytes)),
		Header:     make(http.Header),
	})
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	_, err = pearB.PutRecord(t.Context(), bDID, bDID, coll, val, bRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
	// Manually deliver notification to A's inbox.
	err = pearAC.NotifyOfUpdate(t.Context(), bDID, aDID, coll, bRkey, clique.String())
	require.NoError(t, err)

	// Both A and B can see both records.
	// A can see its own record (local).
	got, err := pearAC.GetRecord(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// A can see B's record via remote fetch (getRecord XRPC 4).
	bRec, err := pearB.getRecordLocal(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	bRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", bDID.String(), coll, bRkey),
		Value: bRec.Value,
	})
	require.NoError(t, err)

	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bRecBodyBytes)),
		Header:     make(http.Header),
	})
	got, err = pearAC.GetRecord(t.Context(), coll, bRkey, bDID, aDID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// A lists both records; remote B record is fetched via inbox notification (getRecord XRPC 5).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bRecBodyBytes)),
		Header:     make(http.Header),
	})
	aRecords, err := pearAC.ListRecords(t.Context(), aDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, aRecords, 2)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// B lists both records.
	// B resolves clique from A (getRecord XRPC 6) then fetches A's record (getRecord XRPC 7).
	aRec, err := pearAC.getRecordLocal(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	aRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, aRkey),
		Value: aRec.Value,
	})
	require.NoError(t, err)

	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(cliqueBodyBytes)),
		Header:     make(http.Header),
	})
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecBodyBytes)),
		Header:     make(http.Header),
	})
	bRecords, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecords, 2)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// A adds C to the clique (C is local to pearAC, no XRPC needed).
	_, err = pearAC.AddPermissions(
		t.Context(),
		aDID,
		[]permissions.Grantee{permissions.DIDGrantee(cDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	)
	require.NoError(t, err)

	// C should have a notification from B (delivered via inbox when C was added).
	notifs, err := pearAC.inbox.GetCollectionUpdatesByRecipient(t.Context(), cDID, coll)
	require.NoError(t, err)
	require.Len(t, notifs, 1)
	require.Equal(t, notifs[0].Sender, bDID.String())

	// C can see both records.
	// C sees A's record locally (same pear).
	got, err = pearAC.GetRecord(t.Context(), coll, aRkey, aDID, cDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// C fetches B's record remotely (getRecord XRPC 8).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bRecBodyBytes)),
		Header:     make(http.Header),
	})
	got, err = pearAC.GetRecord(t.Context(), coll, bRkey, bDID, cDID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// C lists both records; B's record is fetched via inbox notification (getRecord XRPC 9).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bRecBodyBytes)),
		Header:     make(http.Header),
	})
	cRecords, err := pearAC.ListRecords(t.Context(), cDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, cRecords, 2)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// A removes B from the clique.
	require.NoError(t, pearAC.permissions.RemovePermissions(
		[]permissions.Grantee{permissions.DIDGrantee(bDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	))

	// B can no longer see A's record (getRecord XRPC 10 returns forbidden).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(&bytes.Buffer{}),
	})
	got, err = pearB.GetRecord(t.Context(), coll, aRkey, aDID, bDID)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrUnauthorized)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// B can still see its own record.
	got, err = pearB.GetRecord(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// B can no longer list A's record; only sees its own.
	// Clique resolution returns forbidden (XRPC 11) and A's record fetch returns forbidden (XRPC 12).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(&bytes.Buffer{}),
	}).Times(2)
	bRecordsAfterRemoval, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecordsAfterRemoval, 1)
	require.Equal(t, bDID.String(), bRecordsAfterRemoval[0].Did)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
}

// TestAddPermissionsCollectionLevel verifies that granting whole-collection access via
// AddPermissions(rkey="") allows the grantee to see all records in that collection via ListRecords.
func TestMultiAddPermissionsCollectionLevel(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")

	// A and B are on the same pear — no XRPC needed for local permission checks.
	mockXRPCs := newMockXrpcChannel(t)
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID}, []syntax.DID{bDID})
	pearA, pearB := ps[0], ps[1]

	coll := syntax.NSID("my.fake.collection")
	validate := true
	val1 := map[string]any{"data": "value1"}
	val2 := map[string]any{"data": "value2"}

	_, err := pearA.PutRecord(t.Context(), aDID, aDID, coll, val1, "rec-1", &validate, []permissions.Grantee{})
	require.NoError(t, err)
	_, err = pearA.PutRecord(t.Context(), aDID, aDID, coll, val2, "rec-2", &validate, []permissions.Grantee{})
	require.NoError(t, err)

	// Grant B whole-collection access.
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	_, err = pearA.AddPermissions(t.Context(), aDID, []permissions.Grantee{permissions.DIDGrantee(bDID)}, aDID, coll, "")
	require.NoError(t, err)
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, "", "added_permission")
	require.NoError(t, err)

	aRecordsBody, err := json.Marshal(&habitat.NetworkHabitatRepoListRecordsOutput{
		Records: []habitat.NetworkHabitatRepoListRecordsRecord{
			{Uri: fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, "rec-1"), Value: val1},
			{Uri: fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, "rec-2"), Value: val2},
		},
	})
	require.NoError(t, err)
	mockXRPCs.Expect("listRecords").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecordsBody)),
		Header:     make(http.Header),
	})

	records, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, r := range records {
		require.Equal(t, aDID.String(), r.Did)
	}
}

// TestDIDGranteeCrossNodeNotification verifies that when A puts a record with a direct DIDGrantee(B)
// where B is on a remote node, PutRecord sends an XRPC notifyOfUpdate to B's node, and B can
// subsequently see A's record via ListRecords.
func TestMultiDIDGranteeCrossNodeNotification(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")

	mockXRPCs := newMockXrpcChannel(t)
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID}, []syntax.DID{bDID})
	pearA, pearB := ps[0], ps[1]

	coll := syntax.NSID("my.fake.collection")
	aRkey := syntax.RecordKey("a-record")
	validate := true
	val := map[string]any{"data": "value"}

	// A puts a record granting B direct access. PutRecord should notify B's node via XRPC.
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	_, err := pearA.PutRecord(t.Context(), aDID, aDID, coll, val, aRkey, &validate, []permissions.Grantee{permissions.DIDGrantee(bDID)})
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// Manually deliver the notification to B's inbox (simulating B's node receiving the XRPC).
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, aRkey, "added_permission")
	require.NoError(t, err)

	// B calls ListRecords; it fetches A's record via getRecord XRPC.
	aRec, err := pearA.getRecordLocal(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	aRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, aRkey),
		Value: aRec.Value,
	})
	require.NoError(t, err)

	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecBodyBytes)),
		Header:     make(http.Header),
	})
	bRecords, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecords, 1)
	require.Equal(t, aDID.String(), bRecords[0].Did)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
}

// TestAddCliqueThirdNodeFanOut verifies that when A adds C (on a third remote node) to an
// existing clique, C receives notifications about all existing clique records and can then
// call ListRecords to see them.
func TestMultiAddCliqueThirdNodeFanOut(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	mockXRPCs := newMockXrpcChannel(t)
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID}, []syntax.DID{bDID}, []syntax.DID{cDID})
	pearA, pearB, pearC := ps[0], ps[1], ps[2]

	coll := syntax.NSID("my.fake.collection")
	aRkey := syntax.RecordKey("rec-a")
	bRkey := syntax.RecordKey("rec-b")
	validate := true
	val := map[string]any{"data": "value"}

	cliqueRkey := syntax.RecordKey("shared-clique")
	clique := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), cliqueRkey.String()))
	cliqueMembers := []permissions.Grantee{permissions.DIDGrantee(bDID)}

	// Seed: A creates a clique with B as a member (low-level, no XRPC).
	_, err := pearA.permissions.AddPermissions(cliqueMembers, aDID, permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)

	// A puts rec-a shared with the clique. PutRecord notifies B (XRPC 1).
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	_, err = pearA.PutRecord(t.Context(), aDID, aDID, coll, val, aRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
	// Manually deliver to B's inbox.
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, aRkey, clique.String())
	require.NoError(t, err)

	// B puts rec-b shared with the clique.
	// PutRecord resolves clique from A (getRecord XRPC 2), then notifies A (notifyOfUpdate XRPC 3).
	cliqueBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:         fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), permissions.CliqueNSID, cliqueRkey),
		Value:       map[string]any{},
		Permissions: permissions.ConstructInterfaceFromGrantees(cliqueMembers),
	})
	require.NoError(t, err)
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(cliqueBodyBytes)),
		Header:     make(http.Header),
	})
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	_, err = pearB.PutRecord(t.Context(), bDID, bDID, coll, val, bRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
	// Manually deliver to A's inbox.
	err = pearA.NotifyOfUpdate(t.Context(), bDID, aDID, coll, bRkey, clique.String())
	require.NoError(t, err)

	// A adds C to the clique. C is remote, so A must send notifyOfUpdate for each existing clique
	// record (rec-a from A's local store, rec-b from A's inbox).
	// Expected: XRPC 4 notifyOfUpdate to C for rec-a, XRPC 5 notifyOfUpdate to C for rec-b.
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{}).Times(2)
	_, err = pearA.AddPermissions(
		t.Context(),
		aDID,
		[]permissions.Grantee{permissions.DIDGrantee(cDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	)
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// Manually deliver both notifications to C's inbox.
	err = pearC.NotifyOfUpdate(t.Context(), aDID, cDID, coll, aRkey, clique.String())
	require.NoError(t, err)
	err = pearC.NotifyOfUpdate(t.Context(), bDID, cDID, coll, bRkey, clique.String())
	require.NoError(t, err)

	// C calls ListRecords; fetches rec-a from A (XRPC 6) and rec-b from B (XRPC 7).
	aRec, err := pearA.getRecordLocal(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	aRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, aRkey),
		Value: aRec.Value,
	})
	require.NoError(t, err)

	bRec, err := pearB.getRecordLocal(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	bRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", bDID.String(), coll, bRkey),
		Value: bRec.Value,
	})
	require.NoError(t, err)

	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecBodyBytes)),
		Header:     make(http.Header),
	})
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(bRecBodyBytes)),
		Header:     make(http.Header),
	})
	cRecords, err := pearC.ListRecords(t.Context(), cDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, cRecords, 2)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
}

// TestPutRecordMultipleCliqueGrantees verifies that when A puts a record granted to two separate
// cliques simultaneously, both B (clique-1 member) and C (clique-2 member) receive notifications
// and can see the record via ListRecords.
func TestMultiPutRecordMultipleCliqueGrantees(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	mockXRPCs := newMockXrpcChannel(t)
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID}, []syntax.DID{bDID}, []syntax.DID{cDID})
	pearA, pearB, pearC := ps[0], ps[1], ps[2]

	coll := syntax.NSID("my.fake.collection")
	aRkey := syntax.RecordKey("a-record")
	validate := true
	val := map[string]any{"data": "value"}

	clique1Rkey := syntax.RecordKey("clique-1")
	clique2Rkey := syntax.RecordKey("clique-2")
	clique1 := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), clique1Rkey.String()))
	clique2 := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), clique2Rkey.String()))

	// Seed both cliques (low-level, no XRPC).
	_, err := pearA.permissions.AddPermissions([]permissions.Grantee{permissions.DIDGrantee(bDID)}, aDID, permissions.CliqueNSID, clique1Rkey)
	require.NoError(t, err)
	_, err = pearA.permissions.AddPermissions([]permissions.Grantee{permissions.DIDGrantee(cDID)}, aDID, permissions.CliqueNSID, clique2Rkey)
	require.NoError(t, err)

	// A puts a record granted to both cliques. Both cliques are local (A owns them), so no XRPC
	// for clique resolution. A notifies B (XRPC 1) and C (XRPC 2).
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	_, err = pearA.PutRecord(t.Context(), aDID, aDID, coll, val, aRkey, &validate, []permissions.Grantee{clique1, clique2})
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// Manually deliver notifications.
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, aRkey, clique1.String())
	require.NoError(t, err)
	err = pearC.NotifyOfUpdate(t.Context(), aDID, cDID, coll, aRkey, clique2.String())
	require.NoError(t, err)

	// Prepare A's record response body for XRPC fetches.
	aRec, err := pearA.getRecordLocal(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	aRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, aRkey),
		Value: aRec.Value,
	})
	require.NoError(t, err)

	// B calls ListRecords; fetches A's record (XRPC 3).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecBodyBytes)),
		Header:     make(http.Header),
	})
	bRecords, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecords, 1)
	require.Equal(t, aDID.String(), bRecords[0].Did)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// C calls ListRecords; fetches A's record (XRPC 4).
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecBodyBytes)),
		Header:     make(http.Header),
	})
	cRecords, err := pearC.ListRecords(t.Context(), cDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, cRecords, 1)
	require.Equal(t, aDID.String(), cRecords[0].Did)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
}

// TestPutCliqueRecordFansOutNotifications verifies that when A creates a new clique record that
// includes B, and A already has records shared with that clique, B is notified about those
// existing records so B can see them via ListRecords.
// NOTE: This test will fail until pear.go:PutRecord fans out notifications for existing records
// when a CliqueNSID record is created.
func TestMultiPutCliqueRecordFansOutNotifications(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")

	mockXRPCs := newMockXrpcChannel(t)
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID}, []syntax.DID{bDID})
	pearA, pearB := ps[0], ps[1]

	coll := syntax.NSID("my.fake.collection")
	rec1Rkey := syntax.RecordKey("rec-1")
	validate := true
	val := map[string]any{"data": "value"}

	cliqueRkey := syntax.RecordKey("shared-clique")
	clique := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), cliqueRkey.String()))

	// A stores rec-1 shared with the (not-yet-created) clique via low-level store.
	_, err := pearA.PutRecord(t.Context(), aDID, aDID, coll, val, rec1Rkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)

	// A creates the clique by calling PutRecord(CliqueNSID) with B as a member.
	// This should trigger a notifyOfUpdate to B for the existing rec-1.
	// XRPC 1: notifyOfUpdate to B for rec-1.
	mockXRPCs.Expect("notifyOfUpdate").WillReturn(&http.Response{})
	cliqueMembers := []permissions.Grantee{permissions.DIDGrantee(bDID)}
	_, err = pearA.PutRecord(t.Context(), aDID, aDID, permissions.CliqueNSID, map[string]any{}, cliqueRkey, &validate, cliqueMembers)
	require.NoError(t, err)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())

	// Manually deliver the notification to B's inbox.
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, rec1Rkey, clique.String())
	require.NoError(t, err)

	// B calls ListRecords; fetches rec-1 from A (XRPC 2).
	aRec, err := pearA.getRecordLocal(t.Context(), coll, rec1Rkey, aDID, aDID)
	require.NoError(t, err)
	aRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", aDID.String(), coll, rec1Rkey),
		Value: aRec.Value,
	})
	require.NoError(t, err)

	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(aRecBodyBytes)),
		Header:     make(http.Header),
	})
	bRecords, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecords, 1)
	require.Equal(t, aDID.String(), bRecords[0].Did)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
}

// TestListRecordsSubjectsFilterMultiPear verifies the behavior of the subjects filter in
// ListRecords across local and remote records. Local records are filtered by subjects (only
// records owned by a DID in subjects are returned), but remote records sourced from the inbox
// are returned regardless of the subjects filter.
func TestMultiListRecordsSubjectsFilterMultiPear(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	mockXRPCs := newMockXrpcChannel(t)
	// pearAB serves both A and B locally. C is unknown to this pear (remote).
	ps := newMultiPears(t, mockXRPCs, []syntax.DID{aDID, bDID})
	pearAB := ps[0]

	coll := syntax.NSID("my.fake.collection")
	validate := true
	val := map[string]any{"data": "value"}
	cRkey := syntax.RecordKey("rec-c")

	// A puts two records (no grantees — private to A).
	_, err := pearAB.PutRecord(t.Context(), aDID, aDID, coll, val, "rec-a1", &validate, []permissions.Grantee{})
	require.NoError(t, err)
	_, err = pearAB.PutRecord(t.Context(), aDID, aDID, coll, val, "rec-a2", &validate, []permissions.Grantee{})
	require.NoError(t, err)

	// B puts a record granted to A (local, no XRPC).
	_, err = pearAB.PutRecord(t.Context(), bDID, bDID, coll, val, "rec-b1", &validate, []permissions.Grantee{permissions.DIDGrantee(aDID)})
	require.NoError(t, err)

	// Simulate C's node sending A a notification about a remote record.
	err = pearAB.NotifyOfUpdate(t.Context(), cDID, aDID, coll, cRkey, "")
	require.NoError(t, err)

	// Prepare C's record response body.
	cRecBodyBytes, err := json.Marshal(&habitat.NetworkHabitatRepoGetRecordOutput{
		Uri:   fmt.Sprintf("habitat://%s/%s/%s", cDID.String(), coll, cRkey),
		Value: val,
	})
	require.NoError(t, err)

	// A calls ListRecords with subjects=[aDID].
	// Local behavior: only A-owned records are returned (B's rec-b1 is filtered out).
	// Remote behavior: C's record is fetched regardless (subjects filter is not applied to inbox).
	// XRPC 1: getRecord for C's remote record.
	mockXRPCs.Expect("getRecord").WillReturn(&http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(cRecBodyBytes)),
		Header:     make(http.Header),
	})
	subjects := []syntax.DID{aDID}
	records, err := pearAB.ListRecords(t.Context(), aDID, coll, subjects)
	require.NoError(t, err)
	// rec-a1, rec-a2 (A-owned local) + rec-c (remote, subjects ignored) = 3 records.
	require.Len(t, records, 3)
	require.NoError(t, mockXRPCs.ExpectationsWereMet())
}
