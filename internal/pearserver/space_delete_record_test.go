package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestServer_DeleteRecord(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore
	uri, err := store.CreateSpace(t.Context(), org, groupTp, "test")
	require.NoError(t, err)

	// populate seeds a record owned by owner for the subtests to delete.
	populate := func(t *testing.T, rkey string) {
		t.Helper()
		_, _, err = store.PutRecord(
			t.Context(),
			uri,
			owner,
			syntax.NSID("network.habitat.note"),
			syntax.RecordKey(rkey),
			spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
		)
		require.NoError(t, err)
	}

	t.Run("deletes an owned record", func(t *testing.T) {
		populate(t, "del-me")

		var out habitat.NetworkHabitatSpaceDeleteRecordOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.DeleteRecord,
			habitat.NetworkHabitatSpaceDeleteRecordInput{
				Space: uri.String(), Repo: "did:plc:owner",
				Collection: "network.habitat.note", Rkey: "del-me",
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		_, err = store.GetRecord(
			t.Context(),
			uri,
			owner,
			syntax.NSID("network.habitat.note"),
			"del-me",
		)
		require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	})

	t.Run("rejects deleting another repo's record", func(t *testing.T) {
		populate(t, "test")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.DeleteRecord,
			habitat.NetworkHabitatSpaceDeleteRecordInput{
				Space: uri.String(), Repo: "did:plc:alice",
				Collection: "network.habitat.note", Rkey: "test",
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
