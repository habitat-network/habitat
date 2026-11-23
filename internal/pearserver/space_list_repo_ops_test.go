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

	// TestServer_ListRepoOps_IncludesCommit verifies that at the head of the
	// oplog the commit is returned, and that it carries the repo's LtHash.
	t.Run("includes the commit at head", func(t *testing.T) {
		uri := newSpace(t, "head-commit")
		putRecord(t, uri, groupTp, "k1", map[string]any{"x": 1})

		var out habitat.NetworkHabitatSpaceListRepoOpsOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps, url.Values{"space": {uri.String()}, "repo": {owner.String()}}, &out,
		)
		require.Equal(t, http.StatusOK, code)

		require.Len(t, out.Ops, 1)
		require.Equal(t, int64(spacecommit.Version), out.Commit.Ver)
		require.Equal(t, out.Ops[0].Rev, out.Commit.Rev)
		require.Equal(t, owner.String(), out.Commit.Repo)

		_, wantHash, found, err := store.RepoHead(t.Context(), uri, owner)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, wantHash, []byte(out.Commit.Data))
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

	t.Run("cursor continues a paginated response", func(t *testing.T) {
		uri := newSpace(t, "cursor")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"x": 1})
		putRecord(t, uri, coll, "k2", map[string]any{"x": 2})

		client := httpx_testutil.NewTestXRPCClient(t)
		query := func(cursor string) habitat.NetworkHabitatSpaceListRepoOpsOutput {
			var out habitat.NetworkHabitatSpaceListRepoOpsOutput
			params := url.Values{"space": {uri.String()}, "repo": {owner.String()}, "limit": {"1"}}
			if cursor != "" {
				params.Set("cursor", cursor)
			}
			require.Equal(t, http.StatusOK, client.Query(ts.Server.ListRepoOps, params, &out))
			return out
		}

		first := query("")
		require.Len(t, first.Ops, 1)
		require.Equal(t, "k1", first.Ops[0].Rkey)
		second := query(first.Cursor)
		require.Len(t, second.Ops, 1)
		require.Equal(t, "k2", second.Ops[0].Rkey)
		require.Empty(t, query(second.Cursor).Ops)
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

	// A create has no previous cid, which the lexicon says is null: a client
	// that validates the response (the app's space syncer) rejects "" as an
	// invalid CID.
	t.Run("encodes a missing prev as null", func(t *testing.T) {
		uri := newSpace(t, "null-prev")
		coll := syntax.NSID("network.habitat.note")
		putRecord(t, uri, coll, "k1", map[string]any{"v": 1})
		putRecord(t, uri, coll, "k2", map[string]any{"v": 1})
		putRecord(t, uri, coll, "k2", map[string]any{"v": 2})

		var output struct {
			Ops []map[string]any `json:"ops"`
		}
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepoOps,
			url.Values{"space": {uri.String()}, "repo": {owner.String()}},
			&output,
		)
		require.Equal(t, http.StatusOK, code)

		require.Len(t, output.Ops, 2)
		require.Contains(t, output.Ops[0], "prev")
		require.Nil(t, output.Ops[0]["prev"])
		require.NotEmpty(t, output.Ops[0]["cid"])
		// The update of k2 does carry its prev.
		require.NotEmpty(t, output.Ops[1]["prev"])
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
