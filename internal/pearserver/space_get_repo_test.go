package pearserver_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_GetRepo(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	t.Run("returns a host-signed STAR repo", func(t *testing.T) {
		pub, err := ts.HostKey.PublicKey()
		require.NoError(t, err)

		uri, err := store.CreateSpace(t.Context(), org, groupTp, "test")
		require.NoError(t, err)

		coll := syntax.NSID("network.habitat.note")
		_, _, err = store.PutRecord(
			t.Context(),
			uri,
			owner,
			coll,
			"k1",
			spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
		)
		require.NoError(t, err)

		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/com.atproto.space.getRepo?space="+uri.String()+"&repo="+owner.String(),
			http.NoBody,
		)
		w := httptest.NewRecorder()
		ts.Server.GetRepo(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "application/octet-stream", w.Header().Get("Content-Type"))

		reader, err := star.NewReader(bytes.NewReader(w.Body.Bytes()), 0)
		require.NoError(t, err)
		commit := reader.Meta
		require.Equal(t, spaces.RepoStarType, commit["$type"])
		require.Equal(t, int64(spacecommit.Version), commit["ver"])
		require.Equal(t, owner.String(), commit["repo"])
		require.Equal(t, uri.String(), commit["space"])

		path, _, err := reader.Next()
		require.NoError(t, err)
		require.Equal(t, "network.habitat.note/k1", string(path))
		_, _, err = reader.Next()
		require.ErrorIs(t, err, io.EOF)

		rev, ok := commit["rev"].(string)
		require.True(t, ok)
		ikm, ok := commit["ikm"].(atdata.Bytes)
		require.True(t, ok)
		sig, ok := commit["sig"].(atdata.Bytes)
		require.True(t, ok)
		hash, ok := commit["data"].(atdata.Bytes)
		require.True(t, ok)

		// External author (did:plc:owner) → host-signed, so verify with the host key.
		ctxBytes := spacecommit.Ctx(uri, owner, rev, ikm)
		require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

		_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, wantHash, []byte(hash))
	})

	t.Run("returns RepoNotFound for a missing repo", func(t *testing.T) {
		uri, err := store.CreateSpace(t.Context(), org, groupTp, "missing")
		require.NoError(t, err)

		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.GetRepo, url.Values{"space": {uri.String()}, "repo": {alice.String()}}, &apiErr,
		)

		require.Equal(t, http.StatusNotFound, code)
		require.Equal(t, "RepoNotFound", apiErr.Name)
	})

	// TestServer_GetRepo_SpaceNotFound distinguishes an unknown space (400,
	// SpaceNotFound) from a known space with no repo (404, RepoNotFound, above).
	t.Run("returns SpaceNotFound for an unknown space", func(t *testing.T) {
		uri := habitat_syntax.ConstructSpaceURI(owner, groupTp, "nonexistent")

		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.GetRepo, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &apiErr,
		)

		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "SpaceNotFound", apiErr.Name)
	})
}
