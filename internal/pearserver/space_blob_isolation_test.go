package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestSpaceBlobIsolation(t *testing.T) {
	for _, scenario := range []string{"unreferenced", "foreign repo", "reference lifecycle"} {
		t.Run(scenario, func(t *testing.T) {
			ts := pearserver_testutil.NewTestServer(t)
			store := ts.SpaceStore
			space, err := store.CreateSpace(t.Context(), org, groupTp, "blobs")
			require.NoError(t, err)
			other, err := store.CreateSpace(t.Context(), org, groupTp, "other")
			require.NoError(t, err)
			upload := httptest.NewRecorder()
			ts.Server.UploadBlob(upload, httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.repo.uploadBlob",
				strings.NewReader("synthetic private blob"),
			))
			require.Equal(t, http.StatusOK, upload.Code)
			var blob habitat.NetworkHabitatRepoUploadBlobOutput
			require.NoError(t, json.Unmarshal(upload.Body.Bytes(), &blob))
			record := spaces_testutil.MustMarshalRecord(t, map[string]any{
				"$type": groupTp.String(), "blob": blob.Blob,
			})
			read := func(spaceURI string, want int) {
				t.Helper()
				response := httptest.NewRecorder()
				ts.Server.GetBlob(response, httptest.NewRequest(
					http.MethodGet,
					"/xrpc/network.habitat.space.getBlob?space="+url.QueryEscape(
						spaceURI,
					)+"&cid="+blob.Cid,
					http.NoBody,
				))
				require.Equal(t, want, response.Code)
				if want == http.StatusOK {
					require.Equal(t, "synthetic private blob", response.Body.String())
				}
			}
			switch scenario {
			case "unreferenced":
				read(space.String(), http.StatusNotFound)
			case "foreign repo":
				_, _, err := store.PutRecord(t.Context(), other, alice, groupTp, "forged", record)
				require.ErrorIs(t, err, spaces.ErrBlobNotFound)
				_, err = store.GetRecord(t.Context(), other, alice, groupTp, "forged")
				require.ErrorIs(t, err, spaces.ErrRecordNotFound)
				read(other.String(), http.StatusNotFound)
			case "reference lifecycle":
				for _, key := range []syntax.RecordKey{"first", "second"} {
					_, _, err := store.PutRecord(t.Context(), space, owner, groupTp, key, record)
					require.NoError(t, err)
				}
				read(space.String(), http.StatusOK)
				read(other.String(), http.StatusNotFound)
				require.NoError(t, store.DeleteRecord(t.Context(), space, owner, groupTp, "first"))
				read(space.String(), http.StatusOK)
				_, _, err = store.PutRecord(t.Context(), space, owner, groupTp, "second",
					spaces_testutil.MustMarshalRecord(t, map[string]any{"$type": groupTp.String()}))
				require.NoError(t, err)
				read(space.String(), http.StatusNotFound)
			}
		})
	}
}
