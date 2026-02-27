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

func newMultiPears(t *testing.T, aDIDs []syntax.DID, bDIDs []syntax.DID, mockXrpcCh xrpcchannel.XrpcChannel) (*pear, *pear) {
	pearAName := "pearA"
	pearAEndpoint := "https://pearA"

	pearBName := "pearB"
	pearBEndpoint := "https://pearB"

	dirA := identity.NewMockDirectory()
	for _, did := range aDIDs {
		dirA.Insert(identity.Identity{
			DID: did,
			Services: map[string]identity.ServiceEndpoint{
				pearAName: identity.ServiceEndpoint{
					URL: pearAEndpoint,
				},
			},
		})
	}

	dirB := identity.NewMockDirectory()
	for _, did := range bDIDs {
		dirB.Insert(identity.Identity{
			DID: did,
			Services: map[string]identity.ServiceEndpoint{
				pearBName: identity.ServiceEndpoint{
					URL: pearBEndpoint,
				},
			},
		})
	}

	nodeA := node.New(pearAName, pearAEndpoint, dirA, mockXrpcCh)
	nodeB := node.New(pearBName, pearBEndpoint, dirB, mockXrpcCh)

	pearA := newPearForTest(t, dirA, withNode(nodeA))
	pearB := newPearForTest(t, dirB, withNode(nodeB))

	return pearA, pearB
}

func TestCliqueFlowMultiPear(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	mockXRPCs := &mockXrpcChannel{
		actions: []*http.Response{},
	}
	pearAC, pearB := newMultiPears(t, []syntax.DID{aDID, cDID}, []syntax.DID{bDID}, mockXRPCs)

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

	// A creates a record and grants access to the clique
	mockXRPCs.actions = append(mockXRPCs.actions, &http.Response{ /* empty response because notify of update doesn't care about the response */ })
	// mock out the notify of update
	// TODO: hook upt he notify of update calls to the mockXRPC
	err = pearB.NotifyOfUpdate(t.Context(), aDID, bDID, coll, aRkey, clique.String())

	_, err = pearAC.PutRecord(t.Context(), aDID, aDID, coll, val, aRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)
	require.Len(t, mockXRPCs.actions, 0)

	// This is going to make an xrpc request to A's node to notify about updates
	// One request to resolve the clique
	output := &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			aDID.String(),
			permissions.CliqueNSID,
			cliqueRkey,
		),
		// Value is ignored
		Value: map[string]any{},
		Permissions: permissions.ConstructInterfaceFromGrantees(
			cliqueMembers,
		),
	}

	body, err := json.Marshal(output)
	require.NoError(t, err)

	aCliqueResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
	mockXRPCs.actions = append(mockXRPCs.actions, aCliqueResp)
	mockXRPCs.actions = append(mockXRPCs.actions, &http.Response{ /* empty response because notify of update doesn't care about the response */ })

	_, err = pearB.PutRecord(t.Context(), bDID, bDID, coll, val, bRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)
	require.Len(t, mockXRPCs.actions, 0)

	err = pearAC.NotifyOfUpdate(t.Context(), bDID, aDID, coll, bRkey, clique.String())
	require.NoError(t, err)

	// Both A and B can see both records
	// A can see its own record
	got, err := pearAC.GetRecord(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// A can see B's record
	// This is going to call out to B's pear. So mock an xrpc.
	bRec, err := pearB.getRecordLocal(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)

	bRecOutput := &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			bDID.String(),
			coll,
			bRkey,
		),
		Value: bRec.Value,
	}
	output.Value = bRec.Value

	bRecBody, err := json.Marshal(bRecOutput)
	require.NoError(t, err)

	bRecResp := func() *http.Response {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(bytes.NewReader(bRecBody)),
			Header:     make(http.Header),
		}
	}
	mockXRPCs.actions = append(mockXRPCs.actions, bRecResp())

	got, err = pearAC.GetRecord(t.Context(), coll, bRkey, bDID, aDID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, mockXRPCs.actions, 0)

	// Both A and B can list both records via ListRecords
	// A can see both records

	// A is going to fetch out to B to get the record content based on the inbox notification

	mockXRPCs.actions = append(mockXRPCs.actions, bRecResp())
	aRecords, err := pearAC.ListRecords(t.Context(), aDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, aRecords, 2)
	require.Len(t, mockXRPCs.actions, 0)

	// B is going to fetch out to A to get the record content based on the inbox notification
	aRec, err := pearAC.getRecordLocal(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)

	// B is going to fetch the clique from A
	mockXRPCs.actions = append(mockXRPCs.actions, aCliqueResp)

	output = &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			aDID.String(),
			coll,
			aRkey,
		),
		Value: aRec.Value,
	}

	body, err = json.Marshal(output)
	require.NoError(t, err)

	aRecResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
	mockXRPCs.actions = append(mockXRPCs.actions, aRecResp)

	bRecords, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecords, 2)
	require.Len(t, mockXRPCs.actions, 0)

	// A adds C to the clique
	_, err = pearAC.AddPermissions(
		t.Context(),
		aDID,
		[]permissions.Grantee{permissions.DIDGrantee(cDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	)
	require.NoError(t, err)

	// C should have a notifciation from B
	notifs, err := pearAC.inbox.GetCollectionUpdatesByRecipient(t.Context(), cDID, coll)
	require.NoError(t, err)
	require.Len(t, notifs, 1)
	require.Equal(t, notifs[0].Sender, bDID.String())

	// C can see both records
	got, err = pearAC.GetRecord(t.Context(), coll, aRkey, aDID, cDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	mockXRPCs.actions = append(mockXRPCs.actions, bRecResp())
	got, err = pearAC.GetRecord(t.Context(), coll, bRkey, bDID, cDID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Len(t, mockXRPCs.actions, 0)

	// C can also list both records via ListRecords
	mockXRPCs.actions = append(mockXRPCs.actions, bRecResp())
	cRecords, err := pearAC.ListRecords(t.Context(), cDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, cRecords, 2)
	require.Len(t, mockXRPCs.actions, 0)

	// A removes B from the clique
	require.NoError(t, pearAC.permissions.RemovePermissions(
		[]permissions.Grantee{permissions.DIDGrantee(bDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	))

	// B can no longer see A's record
	mockXRPCs.actions = append(mockXRPCs.actions, &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(&bytes.Buffer{}),
	})
	got, err = pearB.GetRecord(t.Context(), coll, aRkey, aDID, bDID)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrUnauthorized)
	require.Len(t, mockXRPCs.actions, 0)

	// B can still see its own record
	got, err = pearB.GetRecord(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// B can no longer list A's record; only sees its own
	// B will resolve the clique
	mockXRPCs.actions = append(mockXRPCs.actions, &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(&bytes.Buffer{}),
	})
	mockXRPCs.actions = append(mockXRPCs.actions, &http.Response{
		StatusCode: http.StatusForbidden,
		Body:       io.NopCloser(&bytes.Buffer{}),
	})
	fmt.Println("-----")
	bRecordsAfterRemoval, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecordsAfterRemoval, 1)
	require.Equal(t, bDID.String(), bRecordsAfterRemoval[0].Did)
	require.Len(t, mockXRPCs.actions, 0)

}
