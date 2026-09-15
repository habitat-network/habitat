package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/habitat-network/habitat/api/habitat"
	httpxtest "github.com/habitat-network/habitat/internal/httpx/testutil"
	peartest "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/ipfs/go-cid"
	"github.com/stretchr/testify/require"
)

func TestMailboxAppendOnlyPreservesBlobAuthorization(t *testing.T) {
	ts := peartest.NewTestServer(t)
	space, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "immutable-blob")
	require.NoError(t, err)
	upload := func(raw string) habitat.NetworkHabitatRepoUploadBlobOutput {
		response := httptest.NewRecorder()
		ts.Server.UploadBlob(
			response,
			httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.repo.uploadBlob",
				strings.NewReader(raw),
			),
		)
		require.Equal(t, http.StatusOK, response.Code)
		var out habitat.NetworkHabitatRepoUploadBlobOutput
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
		return out
	}
	original, replacement := upload(
		"synthetic original immutable bytes",
	), upload(
		"synthetic replacement immutable bytes",
	)
	client := httpxtest.NewTestXRPCClient(t)
	put := habitat.NetworkHabitatSpacePutRecordInput{
		Space:      space.String(),
		Repo:       owner.String(),
		Collection: "email.atmos.message",
		Rkey:       "immutable",
		Record:     map[string]any{"raw": original.Blob},
	}
	var out habitat.NetworkHabitatSpacePutRecordOutput
	require.Equal(t, http.StatusOK, client.Procedure(ts.Server.PutRecord, put, &out))
	var denied struct {
		Error string `json:"error"`
	}
	put.Record = map[string]any{"raw": replacement.Blob}
	require.Equal(t, http.StatusConflict, client.Procedure(ts.Server.PutRecord, put, &denied))
	require.Equal(t, "RecordAlreadyExists", denied.Error)
	require.Equal(
		t,
		http.StatusForbidden,
		client.Procedure(
			ts.Server.DeleteRecord,
			habitat.NetworkHabitatSpaceDeleteRecordInput{
				Space:      space.String(),
				Repo:       owner.String(),
				Collection: put.Collection,
				Rkey:       put.Rkey,
			},
			&denied,
		),
	)
	require.Equal(t, "ImmutableRecord", denied.Error)
	for _, check := range []struct {
		blob habitat.NetworkHabitatRepoUploadBlobOutput
		want bool
	}{{original, true}, {replacement, false}} {
		parsed, err := cid.Parse(check.blob.Cid)
		require.NoError(t, err)
		authorized, err := ts.SpaceStore.SpaceReferencesBlob(t.Context(), space, parsed)
		require.NoError(t, err)
		require.Equal(t, check.want, authorized)
	}
}
