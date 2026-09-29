package pearserver_test

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_ListRepoOps(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	// newSpace creates a fresh space within the shared store, so each subtest
	// operates on isolated data while reusing the single TestServer.
	newSpace := func(t *testing.T, name string) habitat_syntax.SpaceURI {
		t.Helper()
		uri, err := store.CreateSpace(t.Context(), org, groupTp, habitat_syntax.SpaceKey(name))
		require.NoError(t, err)
		return uri
	}

	putRecord := func(t *testing.T, uri habitat_syntax.SpaceURI, coll syntax.NSID, rkey string, rec map[string]any) {
		t.Helper()
		_, _, err := store.PutRecord(
			t.Context(), uri, owner, coll, syntax.RecordKey(rkey),
			spaces_testutil.MustMarshalRecord(t, rec),
		)
		require.NoError(t, err)
	}

	t.Run("lists ops in order with cursor", func(t *testing.T) {
		uri := newSpace(t, "order")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"x": 1})
		putRecord(t, uri, coll, "k2", map[string]any{"x": 2})

		var output habitat.NetworkHabitatSpaceListRepoOpsOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}}, &output,
		)
		require.Equal(t, http.StatusOK, code)

		require.Len(t, output.Ops, 2)
		require.Equal(t, "k1", output.Ops[0].Rkey)
		require.Equal(t, "k2", output.Ops[1].Rkey)
		require.Equal(t, coll.String(), output.Ops[0].Collection)
		require.NotEmpty(t, output.Ops[0].Rev)
		require.NotEmpty(t, output.Cursor)
		require.Equal(t, output.Ops[1].Rev, output.Cursor)
	})

	// TestServer_ListRepoOps_IncludesSignedCommit verifies that at the head of
	// the oplog a host-signed commit is returned, and that it verifies against
	// the host key with the host protocol tag and carries the repo's LtHash.
	t.Run("includes a host-signed commit at head", func(t *testing.T) {
		pub, err := ts.HostKey.PublicKey()
		require.NoError(t, err)

		uri := newSpace(t, "host-signed")
		putRecord(t, uri, groupTp, "k1", map[string]any{"x": 1})

		var out habitat.NetworkHabitatSpaceListRepoOpsOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
		)
		require.Equal(t, http.StatusOK, code)

		require.Len(t, out.Ops, 1)
		require.Equal(t, int64(spacecommit.Version), out.Commit.Ver)
		require.Equal(t, out.Ops[0].Rev, out.Commit.Rev)

		hash := []byte(out.Commit.Hash)
		ikm := []byte(out.Commit.Ikm)
		sig := []byte(out.Commit.Sig)
		require.Len(t, ikm, 32)

		// External author (did:plc:owner) → host-signed, so verify with the host key.
		ctxBytes := spacecommit.Ctx(uri, owner, out.Commit.Rev, ikm)
		require.NoError(t, pub.HashAndVerify(ctxBytes, sig))

		_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, wantHash, hash)
	})

	t.Run("since cursor excludes prior ops", func(t *testing.T) {
		uri := newSpace(t, "since")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"x": 1})
		putRecord(t, uri, coll, "k2", map[string]any{"x": 2})

		client := httpx_testutil.NewTestXRPCClient(t)

		var first habitat.NetworkHabitatSpaceListRepoOpsOutput
		client.Query(
			ts.Server.ListRepoOps,
			url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}},
			&first,
		)
		require.Len(t, first.Ops, 2)

		var second habitat.NetworkHabitatSpaceListRepoOpsOutput
		client.Query(
			ts.Server.ListRepoOps,
			url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}, "since": {first.Cursor}},
			&second,
		)
		require.Len(t, second.Ops, 0)
	})

	t.Run("includes record values by default", func(t *testing.T) {
		uri := newSpace(t, "values")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"text": "hello"})

		var output habitat.NetworkHabitatSpaceListRepoOpsOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {"did:plc:owner"}}, &output,
		)
		require.Equal(t, http.StatusOK, code)

		require.Len(t, output.Ops, 1)
		val, ok := output.Ops[0].Value.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "hello", val["text"])
	})

	t.Run("excludes values when requested", func(t *testing.T) {
		uri := newSpace(t, "exclude")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"text": "hello"})

		var output habitat.NetworkHabitatSpaceListRepoOpsOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps,
			url.Values{
				"space": {uri.String()}, "repo": {"did:plc:owner"}, "excludeValues": {"true"},
			},
			&output,
		)
		require.Equal(t, http.StatusOK, code)

		require.Len(t, output.Ops, 1)
		require.Equal(t, "k1", output.Ops[0].Rkey)
		require.Nil(t, output.Ops[0].Value)
	})

	// TestServer_ListRepoOpsSinceAheadRejects pins that a since beyond the repo
	// head is an error (not an empty page), so an ahead-of-host syncer falls back
	// to a full recovery instead of silently stopping.
	t.Run("rejects a since cursor beyond the head", func(t *testing.T) {
		uri := newSpace(t, "ahead")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"v": 1})

		// A TID-like string that sorts after any real TID (base32 is a-z + 2-7).
		ahead := strings.Repeat("z", 13)
		var body atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps,
			url.Values{"space": {uri.String()}, "repo": {owner.String()}, "since": {ahead}},
			&body,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "RevNotFound", body.Name)
	})
}
