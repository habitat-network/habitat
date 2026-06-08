package pear

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newMultiPears(
	t *testing.T,
	aDIDs []syntax.DID,
	bDIDs []syntax.DID,
	mockXrpcCh xrpcchannel.XrpcChannel,
) (pearA, pearB *pear) {
	pearAName := "pearA"
	pearAEndpoint := "https://pearA"

	pearBName := "pearB"
	pearBEndpoint := "https://pearB"

	dirA := identity.NewMockDirectory()
	for _, did := range aDIDs {
		dirA.Insert(identity.Identity{
			DID: did,
			Services: map[string]identity.ServiceEndpoint{
				pearAName: {
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
				pearBName: {
					URL: pearBEndpoint,
				},
			},
		})
	}

	nodeA := node.New(pearAName, pearAEndpoint, dirA, mockXrpcCh)
	nodeB := node.New(pearBName, pearBEndpoint, dirB, mockXrpcCh)

	db, err := gorm.Open(sqlite.Open(":memory:"))
	require.NoError(t, err)
	pearA = newPearForTest(t, db, dirA, withNode(nodeA))
	pearB = newPearForTest(t, db, dirB, withNode(nodeB))

	return pearA, pearB
}

// TestGetBlobRemote verifies that when B requests a blob owned by A (on a remote node),
// GetBlob forwards the request via a network.habitat.repo.getBlob XRPC and returns the blob data.
func TestGetBlobRemote(t *testing.T) {
	aDID := syntax.DID("did:example:a")
	bDID := syntax.DID("did:example:b")

	mockXRPCs := &mockXrpcChannel{actions: []*http.Response{}}
	pearA, pearB := newMultiPears(t, []syntax.DID{aDID}, []syntax.DID{bDID}, mockXRPCs)

	// A uploads a blob to their local repo.
	blobData := []byte("remote blob content")
	mimeType := "text/plain"
	bmeta, err := pearA.UploadBlob(t.Context(), aDID, aDID, blobData, mimeType)
	require.NoError(t, err)
	cid := syntax.CID(bmeta.Ref.String())

	header := make(http.Header)
	header.Set("Content-Type", mimeType)
	header.Set("Content-Length", fmt.Sprint(len(blobData)))
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(blobData)),
		Header:     header,
	}
	mockXRPCs.actions = append(mockXRPCs.actions, resp) //nolint:bodyclose // mock handler consumes

	gotMime, contentLen, gotBlob, err := pearB.GetBlob(t.Context(), bDID, aDID, cid)
	require.NoError(t, err)
	require.Equal(t, mimeType, gotMime)

	slurp, err := io.ReadAll(gotBlob)
	require.NoError(t, err)
	require.Equal(t, blobData, slurp)
	require.Equal(t, contentLen, fmt.Sprint(len(blobData)))
}
