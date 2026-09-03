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

	// TestServer_GetLatestCommit returns a host-signed commit over the repo's
	// head that verifies against the host key and carries the repo's LtHash.
	t.Run("returns a host-signed commit at head", func(t *testing.T) {
		pub, err := ts.HostKey.PublicKey()
		require.NoError(t, err)

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

		hash := []byte(out.Commit.Hash)
		ikm := []byte(out.Commit.Ikm)
		sig := []byte(out.Commit.Sig)
		require.Len(t, ikm, 32)

		ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
		require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

		rev, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, wantHash, hash)
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
