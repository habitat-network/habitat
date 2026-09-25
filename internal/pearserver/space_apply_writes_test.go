package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpxtest "github.com/habitat-network/habitat/internal/httpx/testutil"
	peartest "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestApplyWritesAtomicAPI(t *testing.T) {
	ts := peartest.NewTestServer(t)
	space, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "batch")
	require.NoError(t, err)
	write := func(key string) map[string]any {
		return map[string]any{
			"action":     "create",
			"collection": "email.atmos.messageState",
			"rkey":       key,
			"value":      map[string]any{"$type": "email.atmos.messageState", "value": "synthetic"},
		}
	}
	input := func(repo string, writes ...map[string]any) map[string]any {
		return map[string]any{"space": space.String(), "repo": repo, "writes": writes}
	}
	client := httpxtest.NewTestXRPCClient(t)
	var rejected map[string]any
	var out struct{ Results []struct{ URI, CID string } }
	status := client.Procedure(
		ts.Server.ApplyWrites,
		input(owner.String(), write("message"), write("state")),
		&out,
	)
	require.Equal(t, http.StatusOK, status)
	require.Len(t, out.Results, 2)
	require.NotEmpty(t, out.Results[0].CID)
	status = client.Procedure(
		ts.Server.ApplyWrites,
		input(owner.String(), write("rollback"), write("state")),
		&rejected,
	)
	require.Equal(t, http.StatusConflict, status)
	_, err = ts.SpaceStore.GetRecord(
		t.Context(),
		space,
		owner,
		"email.atmos.messageState",
		"rollback",
	)
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	status = client.Procedure(
		ts.Server.ApplyWrites,
		input("did:plc:other", write("foreign")),
		&rejected,
	)
	require.Equal(t, http.StatusForbidden, status)
	for _, bad := range []map[string]any{
		{"action": "update", "collection": "email.atmos.messageState", "rkey": "state", "value": map[string]any{"value": "bad"}},
		{"action": "create", "collection": "network.habitat.space.appAccess", "rkey": "relation", "value": map[string]any{"value": "bad"}},
		{"action": "create", "collection": "email.atmos.messageState", "rkey": "state", "value": map[string]any{"value": 1.5}},
	} {
		status = client.Procedure(
			ts.Server.ApplyWrites,
			input(owner.String(), write("must-not-exist"), bad),
			&rejected,
		)
		require.GreaterOrEqual(t, status, 400)
		_, err = ts.SpaceStore.GetRecord(
			t.Context(),
			space,
			owner,
			"email.atmos.messageState",
			"must-not-exist",
		)
		require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	}
	denied := peartest.NewTestServer(t, peartest.WithValidator(authntest.NewFailureValidator()))
	require.Equal(
		t,
		http.StatusUnauthorized,
		client.Procedure(
			denied.Server.ApplyWrites,
			input(owner.String(), write("denied")),
			&rejected,
		),
	)
}

func TestApplyWritesRequestBounds(t *testing.T) {
	ts := peartest.NewTestServer(t)
	for _, body := range []string{
		`{"space":"x","repo":"did:plc:test","writes":[],"unexpected":true}`,
		`{} {}`,
		`{"writes":[],"space":"` + strings.Repeat("x", 1<<20) + `"}`,
	} {
		response := httptest.NewRecorder()
		ts.Server.ApplyWrites(
			response,
			httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.space.applyWrites",
				strings.NewReader(body),
			),
		)
		require.Equal(t, http.StatusBadRequest, response.Code)
	}
	response := httptest.NewRecorder()
	ts.Server.ApplyWrites(
		response,
		httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.applyWrites", http.NoBody),
	)
	require.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestApplyWritesRollbackRemovesBlobAuthorization(t *testing.T) {
	ts := peartest.NewTestServer(t)
	space, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "batch-blob")
	require.NoError(t, err)
	upload := httptest.NewRecorder()
	ts.Server.UploadBlob(
		upload,
		httptest.NewRequest(
			http.MethodPost,
			"/xrpc/network.habitat.repo.uploadBlob",
			strings.NewReader("synthetic atomic bytes"),
		),
	)
	require.Equal(t, http.StatusOK, upload.Code)
	var blob habitat.NetworkHabitatRepoUploadBlobOutput
	require.NoError(t, json.Unmarshal(upload.Body.Bytes(), &blob))
	client := httpxtest.NewTestXRPCClient(t)
	input := map[string]any{
		"space": space.String(),
		"repo":  owner.String(),
		"writes": []map[string]any{
			{
				"action":     "create",
				"collection": "email.atmos.messageState",
				"rkey":       "first",
				"value":      map[string]any{"raw": blob.Blob},
			},
			{
				"action":     "create",
				"collection": "email.atmos.messageState",
				"rkey":       "second",
				"value": map[string]any{
					"raw": map[string]any{
						"$type": "blob",
						"ref": map[string]any{
							"$link": "bafkreigh2akiscaildc6g2s3ux3s2wefqxjrphy5dlowiqzfpczg5si7xe",
						},
						"mimeType": "text/plain",
						"size":     1,
					},
				},
			},
		},
	}
	var rejected map[string]any
	require.Equal(t, http.StatusNotFound, client.Procedure(ts.Server.ApplyWrites, input, &rejected))
	c, err := cid.Parse(blob.Cid)
	require.NoError(t, err)
	referenced, err := ts.SpaceStore.BlobReferenced(t.Context(), space, c)
	require.NoError(t, err)
	require.False(t, referenced, "rollback must revoke the first record's live blob reference")
	_, err = ts.SpaceStore.GetRecord(t.Context(), space, owner, "email.atmos.messageState", "first")
	require.ErrorIs(t, err, spaces.ErrRecordNotFound)
}
