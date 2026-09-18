package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_NotifyWrite(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	newSpace := func(t *testing.T, name string) habitat_syntax.SpaceURI {
		t.Helper()
		uri, err := store.CreateSpace(t.Context(), org, groupTp, habitat_syntax.SpaceKey(name))
		require.NoError(t, err)
		return uri
	}

	t.Run("registers a repo written directly at its own PDS", func(t *testing.T) {
		uri := newSpace(t, "remote-write")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.NotifyWrite,
			habitat.NetworkHabitatSpaceNotifyWriteInput{
				Space: uri.String(), Repo: "did:plc:owner",
				Rev: "3lrev", Hash: atdata.Bytes("some-digest-bytes-xx"),
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)

		repos, err := store.ListRepos(t.Context(), uri)
		require.NoError(t, err)
		require.Len(t, repos, 1)
		require.Equal(t, "3lrev", repos[0].Rev)
		require.Equal(t, []byte("some-digest-bytes-xx"), repos[0].Hash)
	})

	t.Run("rejects notifying for another repo", func(t *testing.T) {
		uri := newSpace(t, "foreign-repo")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.NotifyWrite,
			habitat.NetworkHabitatSpaceNotifyWriteInput{
				Space: uri.String(), Repo: "did:plc:alice",
				Rev: "3lrev", Hash: atdata.Bytes("digest"),
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	t.Run("space not found", func(t *testing.T) {
		missing := habitat_syntax.ConstructSpaceURI(org, groupTp, "nonexistent")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.NotifyWrite,
			habitat.NetworkHabitatSpaceNotifyWriteInput{
				Space: missing.String(), Repo: "did:plc:owner",
				Rev: "3lrev", Hash: atdata.Bytes("digest"),
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})
}
