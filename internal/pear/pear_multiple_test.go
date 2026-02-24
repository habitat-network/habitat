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

	nodeA := node.New(pearAName, pearAEndpoint, &dirA, mockXrpcCh)
	nodeB := node.New(pearBName, pearBEndpoint, &dirB, mockXrpcCh)

	pearA := newPearForTest(t, &dirA, withNode(nodeA))
	pearB := newPearForTest(t, &dirB, withNode(nodeB))

	return pearA, pearB
}

func TestCliqueFlowMultiPear(t *testing.T) {
	t.Skip("this will fail until we implement remote fetches")

	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")
	cDID := syntax.DID("did:example:c")

	mockXRPCs := &mockXrpcChannel{
		actions: []*http.Response{},
	}
	pearAC, pearB := newMultiPears(t, []syntax.DID{aDID, cDID}, []syntax.DID{bDID}, mockXRPCs)

	cliqueRkey := syntax.RecordKey("shared-clique")
	clique := permissions.CliqueGrantee(habitat_syntax.ConstructHabitatUri(aDID.String(), permissions.CliqueNSID.String(), cliqueRkey.String()))

	// A creates the clique by adding B as a member
	require.NoError(t, pearAC.permissions.AddPermissions(
		[]permissions.Grantee{permissions.DIDGrantee(bDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	))

	val := map[string]any{"data": "value"}
	validate := true
	coll := syntax.NSID("my.fake.collection")
	aRkey := syntax.RecordKey("a-record")
	bRkey := syntax.RecordKey("b-record")

	// A and B both are direct grantees of the clique
	bauthz, err := pearAC.HasPermission(t.Context(), bDID, aDID, permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)
	require.True(t, bauthz)

	aauthz, err := pearAC.HasPermission(t.Context(), bDID, aDID, permissions.CliqueNSID, cliqueRkey)
	require.NoError(t, err)
	require.True(t, aauthz)

	// A creates a record and grants access to the clique
	_, err = pearAC.PutRecord(t.Context(), aDID, aDID, coll, val, aRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)

	// B creates a record and grants access to the same clique
	_, err = pearB.PutRecord(t.Context(), bDID, bDID, coll, val, bRkey, &validate, []permissions.Grantee{clique})
	require.NoError(t, err)

	// Both A and B can see both records
	got, err := pearAC.GetRecord(t.Context(), coll, aRkey, aDID, aDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// This is going to call out to B's pear. So mock an xrpc.
	bRec, err := pearB.getRecordLocal(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	output := &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			bDID.String(),
			coll,
			bRkey,
		),
		Value: bRec.Value,
	}
	output.Value = bRec.Value

	body, err := json.Marshal(output)
	require.NoError(t, err)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
	mockXRPCs.actions = append(mockXRPCs.actions, resp)

	got, err = pearAC.GetRecord(t.Context(), coll, bRkey, bDID, aDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// This is going to call out to A's pear. So mock an xrpc.
	aRec, err := pearB.getRecordLocal(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	output = &habitat.NetworkHabitatRepoGetRecordOutput{
		Uri: fmt.Sprintf(
			"habitat://%s/%s/%s",
			aDID.String(),
			coll,
			aRkey,
		),
		Value: aRec.Value,
	}
	output.Value = aRec.Value
	body, err = json.Marshal(output)
	require.NoError(t, err)

	resp = &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(body)),
		Header:     make(http.Header),
	}
	mockXRPCs.actions = append(mockXRPCs.actions, resp)

	// Both A and B can list both records via ListRecords
	aRecords, err := pearAC.ListRecords(t.Context(), aDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, aRecords, 2)

	bRecords, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecords, 2)

	// A adds C to the clique
	require.NoError(t, pearAC.permissions.AddPermissions(
		[]permissions.Grantee{permissions.DIDGrantee(cDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	))

	// C can see both records
	got, err = pearAC.GetRecord(t.Context(), coll, aRkey, aDID, cDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	got, err = pearAC.GetRecord(t.Context(), coll, bRkey, bDID, cDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// C can also list both records via ListRecords
	cRecords, err := pearAC.ListRecords(t.Context(), cDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, cRecords, 2)

	// A removes B from the clique
	require.NoError(t, pearAC.permissions.RemovePermissions(
		[]permissions.Grantee{permissions.DIDGrantee(bDID)},
		aDID,
		permissions.CliqueNSID,
		cliqueRkey,
	))

	// B can no longer see A's record
	got, err = pearB.GetRecord(t.Context(), coll, aRkey, aDID, bDID)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrUnauthorized)

	// B can still see its own record
	got, err = pearB.GetRecord(t.Context(), coll, bRkey, bDID, bDID)
	require.NoError(t, err)
	require.NotNil(t, got)

	// B can no longer list A's record; only sees its own
	bRecordsAfterRemoval, err := pearB.ListRecords(t.Context(), bDID, coll, nil)
	require.NoError(t, err)
	require.Len(t, bRecordsAfterRemoval, 1)
	require.Equal(t, bDID.String(), bRecordsAfterRemoval[0].Did)
}
