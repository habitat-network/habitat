package pearserver_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_UploadBlob(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore
	uri, err := store.CreateSpace(t.Context(), org, groupTp, "blobs")
	require.NoError(t, err)

	// TestServer_UploadAndGetBlob exercises the raw-body upload/download
	// endpoints, which carry non-JSON payloads and thus can't go through
	// TestXRPCClient.
	t.Run("uploads and downloads a blob through the space", func(t *testing.T) {
		// Upload a blob.
		upReq := httptest.NewRequest(
			http.MethodPost,
			"/xrpc/network.habitat.repo.uploadBlob",
			strings.NewReader("hello blobs"),
		)
		upReq.Header.Set("Content-Type", "text/plain")
		upW := httptest.NewRecorder()
		ts.Server.UploadBlob(upW, upReq)
		require.Equal(t, http.StatusOK, upW.Code)

		var out habitat.NetworkHabitatRepoUploadBlobOutput
		require.NoError(t, json.NewDecoder(upW.Body).Decode(&out))
		require.NotEmpty(t, out.Cid)

		// Get it back through the space.
		getW := httptest.NewRecorder()
		ts.Server.GetBlob(
			getW,
			httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.getBlob?space="+
				url.QueryEscape(uri.String())+"&cid="+out.Cid, http.NoBody),
		)

		require.Equal(t, http.StatusOK, getW.Code)
		require.Equal(t, "text/plain", getW.Header().Get("Content-Type"))
		body, err := io.ReadAll(getW.Body)
		require.NoError(t, err)
		require.Equal(t, "hello blobs", string(body))
	})

	t.Run("rejects an oversized blob", func(t *testing.T) {
		// 500 KiB upload limit + 1 byte must be rejected.
		oversized := make([]byte, 500*1024+1)
		upReq := httptest.NewRequest(
			http.MethodPost,
			"/xrpc/network.habitat.repo.uploadBlob",
			bytes.NewReader(oversized),
		)
		upReq.Header.Set("Content-Type", "application/octet-stream")
		upW := httptest.NewRecorder()
		ts.Server.UploadBlob(upW, upReq)

		require.Equal(t, http.StatusRequestEntityTooLarge, upW.Code)
	})
}
