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

func TestServer_ListSpaces(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore

	uri, err := store.CreateSpace(t.Context(), org, groupTp, "my-space")
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

	var output habitat.NetworkHabitatSpaceListSpacesOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(ts.Server.ListSpaces, url.Values{}, &output)

	require.Equal(t, http.StatusOK, code)
	require.Len(t, output.Spaces, 1)
	require.Equal(t, uri.String(), output.Spaces[0].Uri)
}
