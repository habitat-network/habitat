package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_PutRecord(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	// newSpace creates a distinct space per subtest so they don't interfere on
	// the shared store while still reusing the single TestServer.
	newSpace := func(t *testing.T, name string) habitat_syntax.SpaceURI {
		t.Helper()
		uri, err := store.CreateSpace(t.Context(), org, groupTp, habitat_syntax.SpaceKey(name))
		require.NoError(t, err)
		return uri
	}

	t.Run("puts and gets a record", func(t *testing.T) {
		uri := newSpace(t, "roundtrip")
		client := httpx_testutil.NewTestXRPCClient(t)

		var putOutput habitat.NetworkHabitatSpacePutRecordOutput
		putCode := client.Procedure(
			ts.Server.PutRecord,
			habitat.NetworkHabitatSpacePutRecordInput{
				Space: uri.String(), Repo: "did:plc:owner",
				Collection: "network.habitat.note", Rkey: "my-note",
				Record: map[string]any{"text": "hello"},
			},
			&putOutput,
		)
		require.Equal(t, http.StatusOK, putCode)
		require.Contains(t, putOutput.Uri, "/network.habitat.note/my-note")

		var getOutput habitat.NetworkHabitatSpaceGetRecordOutput
		getCode := client.Query(
			ts.Server.GetRecord,
			url.Values{
				"space": {uri.String()}, "collection": {"network.habitat.note"},
				"rkey": {"my-note"}, "repo": {"did:plc:owner"},
			},
			&getOutput,
		)

		require.Equal(t, http.StatusOK, getCode)
		require.Equal(t, putOutput.Uri, getOutput.Uri)
		val, ok := getOutput.Value.(map[string]any)
		require.True(t, ok)
		require.Equal(t, "hello", val["text"])
	})

	t.Run("rejects writing to another repo", func(t *testing.T) {
		uri := newSpace(t, "foreign-repo")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.PutRecord,
			habitat.NetworkHabitatSpacePutRecordInput{
				Space: uri.String(), Repo: "did:plc:alice",
				Collection: "network.habitat.note", Rkey: "test",
				Record: map[string]any{"x": 1},
			},
			&out,
		)
		require.Equal(t, http.StatusBadRequest, code)
	})

	// TestServer_PutRecord_InvalidRecord pins that a record violating the
	// atproto data model (e.g. a non-integer float, which atproto only
	// represents as integers) surfaces as a 400 InvalidRequest, not a 500.
	t.Run("rejects an invalid record", func(t *testing.T) {
		uri := newSpace(t, "invalid-record")

		var out struct{}
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.PutRecord,
			habitat.NetworkHabitatSpacePutRecordInput{
				Space: uri.String(), Repo: "did:plc:owner",
				Collection: "network.habitat.note", Rkey: "my-note",
				Record: map[string]any{"x": 0.15},
			},
			&out,
		)

		require.Equal(t, http.StatusBadRequest, code)

		_, err := store.GetRecord(
			t.Context(),
			uri,
			owner,
			syntax.NSID("network.habitat.note"),
			"my-note",
		)
		require.ErrorIs(t, err, spaces.ErrRecordNotFound)
	})
}
