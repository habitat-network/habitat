package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestServer_GetLatestCommit(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	// TestServer_GetLatestCommit returns the commit over the repo's head: its
	// repo, space and the repo's LtHash digest.
	t.Run("returns the commit at head", func(t *testing.T) {
		uri, err := store.CreateSpace(t.Context(), org, groupTp, "test")
		require.NoError(t, err)
		_, _, err = store.PutRecord(
			t.Context(),
			uri,
			owner,
			groupTp,
			"k1",
			spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
		)
		require.NoError(t, err)

		var out habitat.NetworkHabitatSpaceGetLatestCommitOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.GetLatestCommit, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
		)
		require.Equal(t, http.StatusOK, code)

		require.Equal(t, int64(spacecommit.Version), out.Commit.Ver)
		require.Equal(t, owner.String(), out.Commit.Repo)
		require.Equal(t, uri.String(), out.Commit.Space)

		rev, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, wantHash, []byte(out.Commit.Data))
		require.Equal(t, rev, out.Commit.Rev)
	})

	// TestServer_GetLatestCommit_EmptyRepo returns repo-not-found when the repo
	// holds no records in the space.
	t.Run("returns not found for an empty repo", func(t *testing.T) {
		uri, err := store.CreateSpace(t.Context(), org, groupTp, "empty")
		require.NoError(t, err)

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.GetLatestCommit, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
		)
		require.Equal(t, http.StatusNotFound, code)
	})
}
