package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/searchconfig"
)

func TestServer_SearchCollections(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	// The add and remove endpoints return no body.
	var none struct{}

	t.Run("requires community.configure", func(t *testing.T) {
		ts := newOpenSocialServer(t, alice)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		var out habitat.NetworkHabitatSearchListCollectionsOutput
		require.Equal(t, http.StatusUnauthorized, client.Query(
			ts.Server.ListSearchCollections, url.Values{"org": {orgDID}}, &out,
		))
		require.Equal(t, http.StatusUnauthorized, client.Procedure(
			ts.Server.AddSearchCollection,
			habitat.NetworkHabitatSearchAddCollectionInput{
				Org: orgDID, Collection: "com.example.post",
			},
			&none,
		))
		require.Equal(t, http.StatusUnauthorized, client.Procedure(
			ts.Server.RemoveSearchCollection,
			habitat.NetworkHabitatSearchAddCollectionInput{
				Org: orgDID, Collection: "com.example.post",
			},
			&none,
		))
	})

	t.Run("rejects an invalid collection", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		require.Equal(t, http.StatusBadRequest, client.Procedure(
			ts.Server.AddSearchCollection,
			habitat.NetworkHabitatSearchAddCollectionInput{Org: orgDID, Collection: "nope"},
			&none,
		))
	})

	t.Run("admin adds, lists and removes collections", func(t *testing.T) {
		ts := newOpenSocialServer(t, admin)
		orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
		require.NoError(t, err)

		list := func() habitat.NetworkHabitatSearchListCollectionsOutput {
			var out habitat.NetworkHabitatSearchListCollectionsOutput
			require.Equal(t, http.StatusOK, client.Query(
				ts.Server.ListSearchCollections, url.Values{"org": {orgDID}}, &out,
			))
			return out
		}
		change := func(
			h func(http.ResponseWriter, *http.Request),
			collection string,
		) {
			require.Equal(t, http.StatusOK, client.Procedure(
				h,
				habitat.NetworkHabitatSearchAddCollectionInput{
					Org: orgDID, Collection: collection,
				},
				&none,
			))
		}

		out := list()
		require.Empty(t, out.Collections)
		// Defaults are always reported.
		require.Len(t, out.Defaults, len(searchconfig.DefaultCollections))

		change(ts.Server.AddSearchCollection, "com.example.post")
		change(ts.Server.AddSearchCollection, "com.example.post")
		require.Equal(t, []string{"com.example.post"}, list().Collections)

		got, err := ts.SearchConfig.List(t.Context(), syntax.DID(orgDID))
		require.NoError(t, err)
		require.Equal(t, []syntax.NSID{"com.example.post"}, got)

		change(ts.Server.RemoveSearchCollection, "com.example.post")
		require.Empty(t, list().Collections)
	})
}
