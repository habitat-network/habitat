package pearserver_test

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
	"github.com/habitat-network/habitat/internal/searchconfig"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_SearchRecords(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithSearchIndex(searchtest.NewIndex(t)))
	ctx := t.Context()
	coll := syntax.NSID("network.habitat.note")

	// Search only surfaces collections the org configured, plus the defaults.
	_, err := ts.SpaceStore.CreateSpace(ctx, org, opensocial.MembersSpaceType, "self")
	require.NoError(t, err)
	require.NoError(t, ts.SearchConfig.Put(ctx, org, searchconfig.Config{Collection: coll}))

	// putIndexed writes a note and indexes it, as the indexer would, readable
	// by readers.
	putIndexedIn := func(
		coll syntax.NSID,
		space habitat_syntax.SpaceURI,
		rkey syntax.RecordKey,
		text string,
		readers ...syntax.DID,
	) string {
		value := map[string]any{"text": text}
		uri, _, err := ts.SpaceStore.PutRecord(ctx, space, owner, coll, rkey,
			spaces_testutil.MustMarshalRecord(t, value))
		require.NoError(t, err)
		require.NoError(t, ts.SearchIndex.Put(ctx, search.Document{
			URI:        uri,
			Space:      space,
			Repo:       owner,
			Collection: coll,
			Rev:        syntax.NewTIDNow(0),
			Text:       search.ExtractText(value),
			Access:     search.Access{Principals: search.Principals{Users: readers}},
		}))
		return uri.String()
	}
	putIndexed := func(
		space habitat_syntax.SpaceURI,
		rkey syntax.RecordKey,
		text string,
		readers ...syntax.DID,
	) string {
		return putIndexedIn(coll, space, rkey, text, readers...)
	}
	readable, err := ts.SpaceStore.CreateSpace(ctx, org, groupTp, "search-readable")
	require.NoError(t, err)
	other, err := ts.SpaceStore.CreateSpace(ctx, org, groupTp, "search-other")
	require.NoError(t, err)
	readableURI := putIndexed(readable, "k1", "apple pie", owner)
	readableNote := putIndexed(readable, "k2", "apple crumble", owner)
	otherURI := putIndexed(other, "k1", "apple tart", "did:plc:someone")

	client := httpx_testutil.NewTestXRPCClient(t)
	search := func(params url.Values) (int, habitat.NetworkHabitatSpaceSearchRecordsOutput) {
		var out habitat.NetworkHabitatSpaceSearchRecordsOutput
		code := client.Query(ts.Server.SearchRecords, params, &out)
		return code, out
	}
	uris := func(out habitat.NetworkHabitatSpaceSearchRecordsOutput) []string {
		got := []string{}
		for _, r := range out.Records {
			got = append(got, r.Uri)
		}
		return got
	}

	t.Run("searches what the caller can read", func(t *testing.T) {
		code, out := search(url.Values{"q": {"pie"}})
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Records, 1)
		rec := out.Records[0]
		require.Equal(t, readableURI, rec.Uri)
		require.Equal(t, readable.String(), rec.Space)
		require.NotEmpty(t, rec.Cid)
		require.Equal(t, map[string]any{"text": "apple pie"}, rec.Value)
		require.Contains(t, rec.Snippet, "<mark>pie</mark>")

		code, out = search(url.Values{"q": {"apple"}})
		require.Equal(t, http.StatusOK, code)
		require.ElementsMatch(t, []string{readableURI, readableNote}, uris(out))
	})

	t.Run("pages", func(t *testing.T) {
		code, out := search(url.Values{"q": {"apple"}, "limit": {"1"}})
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Records, 1)
		require.NotEmpty(t, out.Cursor)
		first := out.Records[0].Uri

		code, out = search(url.Values{"q": {"apple"}, "limit": {"1"}, "cursor": {out.Cursor}})
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Records, 1)
		require.NotEqual(t, first, out.Records[0].Uri)
		require.Empty(t, out.Cursor)
	})

	t.Run("filters by space, collection and repo", func(t *testing.T) {
		code, out := search(url.Values{
			"q":          {"apple"},
			"space":      {readable.String()},
			"collection": {coll.String()},
			"repo":       {owner.String()},
		})
		require.Equal(t, http.StatusOK, code)
		require.ElementsMatch(t, []string{readableURI, readableNote}, uris(out))

		code, out = search(url.Values{"q": {"apple"}, "collection": {"network.habitat.other"}})
		require.Equal(t, http.StatusOK, code)
		require.Empty(t, out.Records)
	})

	t.Run("only surfaces configured and default collections", func(t *testing.T) {
		hidden := syntax.NSID("network.habitat.hidden")
		hiddenURI := putIndexedIn(hidden, readable, "q1", "quince jelly", owner)
		defaultURI := putIndexedIn(
			searchconfig.DefaultCollections[0], readable, "q2", "quince jelly", owner,
		)

		// Unconfigured collections are left out; defaults are always in.
		code, out := search(url.Values{"q": {"quince"}})
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, []string{defaultURI}, uris(out))

		// Configuring the collection surfaces it, and removing it hides it
		// again.
		require.NoError(t, ts.SearchConfig.Put(ctx, org, searchconfig.Config{Collection: hidden}))
		code, out = search(url.Values{"q": {"quince"}})
		require.Equal(t, http.StatusOK, code)
		require.ElementsMatch(t, []string{defaultURI, hiddenURI}, uris(out))

		require.NoError(t, ts.SearchConfig.Remove(ctx, org, hidden))
		code, out = search(url.Values{"q": {"quince"}})
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, []string{defaultURI}, uris(out))
	})

	t.Run("scopes a space credential to its space", func(t *testing.T) {
		// Shares the first server's storage and index.
		credTS := pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithDB(ts.DB),
			pearserver_testutil.WithFGA(ts.FGA),
			pearserver_testutil.WithSpaceStore(ts.SpaceStore),
			pearserver_testutil.WithSearchIndex(ts.SearchIndex),
			pearserver_testutil.WithValidator(authntest.NewSuccessValidator(&authn.CredentialInfo{
				Space:  other,
				Method: authn.ValidatorMethodSpaceCredential,
			})),
		)
		var out habitat.NetworkHabitatSpaceSearchRecordsOutput
		code := client.Query(credTS.Server.SearchRecords, url.Values{"q": {"apple"}}, &out)
		require.Equal(t, http.StatusOK, code)
		require.Equal(t, []string{otherURI}, uris(out))
	})

	t.Run("rejects bad params", func(t *testing.T) {
		for _, params := range []url.Values{
			{},
			{"q": {"apple"}, "limit": {"101"}},
			{"q": {"apple"}, "collection": {"not an nsid"}},
			{"q": {"apple"}, "space": {"not a space"}},
			{"q": {"apple"}, "cursor": {"junk"}},
		} {
			code, _ := search(params)
			require.Equal(t, http.StatusBadRequest, code, params)
		}
	})

	t.Run("is unsupported without an index", func(t *testing.T) {
		var out habitat.NetworkHabitatSpaceSearchRecordsOutput
		code := client.Query(pearserver_testutil.NewTestServer(t).Server.SearchRecords,
			url.Values{"q": {"apple"}}, &out)
		require.Equal(t, http.StatusNotImplemented, code)
	})
}
