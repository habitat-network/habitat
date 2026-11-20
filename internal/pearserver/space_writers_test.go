package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

// writerView is the part of a listWriters entry these tests inspect.
type writerView struct {
	Type string `json:"$type"`
	Repo string `json:"repo"`
}

func TestServer_RemoveWriterShowsInListWriters(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	store := ts.SpaceStore
	uri, err := store.CreateSpace(t.Context(), org, groupTp, "writers")
	require.NoError(t, err)
	writes := 0
	put := func() {
		t.Helper()
		// Each write differs from the last: an unchanged record is a no-op.
		writes++
		_, _, err := store.PutRecord(t.Context(), uri, owner, "network.habitat.note", "k1",
			spaces_testutil.MustMarshalRecord(t, map[string]any{"x": writes}))
		require.NoError(t, err)
	}
	put()

	list := func(params url.Values) (repos []writerView, cursor string) {
		t.Helper()
		var out struct {
			Repos  []writerView `json:"repos"`
			Cursor string       `json:"cursor"`
		}
		params.Set("space", uri.String())
		code := httpx_testutil.NewTestXRPCClient(t).Query(ts.Server.ListWriters, params, &out)
		require.Equal(t, http.StatusOK, code)
		return out.Repos, out.Cursor
	}
	listRepos := func() []habitat.NetworkHabitatSpaceListReposRepo {
		t.Helper()
		var out habitat.NetworkHabitatSpaceListReposOutput
		code := httpx_testutil.NewTestXRPCClient(t).Query(
			ts.Server.ListRepos, url.Values{"space": {uri.String()}}, &out,
		)
		require.Equal(t, http.StatusOK, code)
		return out.Repos
	}

	repos, before := list(url.Values{})
	require.Equal(t, []writerView{{"com.atproto.space.defs#repoWrite", owner.String()}}, repos)

	var none struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.RemoveWriter,
		comatproto.ComAtprotoSpaceRemoveWriterInput{Space: uri.String(), Repo: owner.String()},
		&none,
	)
	require.Equal(t, http.StatusOK, code)

	// The removal is a new update after the previous space revision, and it
	// leaves the habitat writer listing.
	repos, _ = list(url.Values{"cursor": {before}})
	require.Equal(t, []writerView{{"com.atproto.space.defs#repoRemoved", owner.String()}}, repos)
	require.Empty(t, listRepos())

	// Removing a non-writer, or removing twice, changes nothing.
	for _, repo := range []syntax.DID{owner, "did:plc:stranger"} {
		code = httpx_testutil.NewTestXRPCClient(t).Procedure(
			ts.Server.RemoveWriter,
			comatproto.ComAtprotoSpaceRemoveWriterInput{Space: uri.String(), Repo: repo.String()},
			&none,
		)
		require.Equal(t, http.StatusOK, code)
	}
	_, after := list(url.Values{})
	_, again := list(url.Values{})
	require.Equal(t, after, again)

	// The next authorized write adds the repo back.
	put()
	repos, _ = list(url.Values{})
	require.Equal(t, []writerView{{"com.atproto.space.defs#repoWrite", owner.String()}}, repos)
	require.Len(t, listRepos(), 1)
}
