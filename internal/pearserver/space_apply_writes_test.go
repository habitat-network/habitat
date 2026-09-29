package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

func TestServer_ApplyWrites(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "apply")
	require.NoError(t, err)
	client := httpx_testutil.NewTestXRPCClient(t)

	apply := func(writes ...any) (int, habitat.NetworkHabitatSpaceApplyWritesOutput) {
		var out habitat.NetworkHabitatSpaceApplyWritesOutput
		code := client.Procedure(ts.Server.ApplyWrites,
			habitat.NetworkHabitatSpaceApplyWritesInput{
				Space: uri.String(), Repo: "did:plc:owner", Writes: writes,
			}, &out)
		return code, out
	}

	t.Run("applies a batch and returns typed results", func(t *testing.T) {
		code, out := apply(
			habitat.NetworkHabitatSpaceApplyWritesCreate{
				Collection: "network.habitat.note", Rkey: "a",
				Value: map[string]any{"text": "one"},
			},
			habitat.NetworkHabitatSpaceApplyWritesCreate{
				Collection: "network.habitat.note", Rkey: "b",
				Value: map[string]any{"text": "two"},
			},
			habitat.NetworkHabitatSpaceApplyWritesUpdate{
				Collection: "network.habitat.note", Rkey: "a",
				Value: map[string]any{"text": "uno"},
			},
			habitat.NetworkHabitatSpaceApplyWritesDelete{
				Collection: "network.habitat.note", Rkey: "b",
			},
		)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Results, 4)
		first, ok := out.Results[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "network.habitat.space.applyWrites#createResult", first["$type"])
		require.Contains(t, first["uri"], "/network.habitat.note/a")
		last, ok := out.Results[3].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "network.habitat.space.applyWrites#deleteResult", last["$type"])

		var got habitat.NetworkHabitatSpaceGetRecordOutput
		code = client.Query(ts.Server.GetRecord, url.Values{
			"space": {uri.String()}, "collection": {"network.habitat.note"},
			"rkey": {"a"}, "repo": {"did:plc:owner"},
		}, &got)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, map[string]any{"text": "uno"}, got.Value)
	})

	t.Run("rejects creating an existing record", func(t *testing.T) {
		create := habitat.NetworkHabitatSpaceApplyWritesCreate{
			Collection: "network.habitat.note", Rkey: "dup",
			Value: map[string]any{"text": "x"},
		}
		code, _ := apply(create)
		require.Equal(t, http.StatusOK, code)
		code, _ = apply(create)
		require.Equal(t, http.StatusConflict, code)
	})

	t.Run("rejects updating a missing record", func(t *testing.T) {
		code, _ := apply(habitat.NetworkHabitatSpaceApplyWritesUpdate{
			Collection: "network.habitat.note", Rkey: "missing",
			Value: map[string]any{"text": "x"},
		})
		require.Equal(t, http.StatusNotFound, code)
	})

	t.Run("rejects an unknown write type and a missing rkey", func(t *testing.T) {
		code, _ := apply(map[string]any{"$type": "bogus#nope", "collection": "network.habitat.note"})
		require.Equal(t, http.StatusBadRequest, code)
		code, _ = apply(habitat.NetworkHabitatSpaceApplyWritesDelete{Collection: "network.habitat.note"})
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("rejects writing to another repo", func(t *testing.T) {
		var out struct{}
		code := client.Procedure(ts.Server.ApplyWrites,
			habitat.NetworkHabitatSpaceApplyWritesInput{
				Space: uri.String(), Repo: "did:plc:alice", Writes: []interface{}{},
			}, &out)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
