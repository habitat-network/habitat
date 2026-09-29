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
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestServer_ListRepos(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	uri, err := store.CreateSpace(t.Context(), org, groupTp, "shared")
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

	var output habitat.NetworkHabitatSpaceListReposOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		ts.Server.ListRepos, url.Values{"space": {uri.String()}}, &output,
	)

	require.Equal(t, http.StatusOK, code)
	require.Len(t, output.Repos, 1)
	require.Equal(t, "did:plc:owner", output.Repos[0].Did)
	require.NotEmpty(t, output.Repos[0].Rev)
	require.NotEmpty(t, output.Repos[0].Hash)
}
