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
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/search"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_SearchRecords(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	ctx := t.Context()
	coll := syntax.NSID("network.habitat.note")

	// putIndexed writes a note and indexes it, as the indexer would.
	putIndexed := func(space habitat_syntax.SpaceURI, rkey syntax.RecordKey, text string) string {
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
		}))
		return uri.String()
	}
	readable, err := ts.SpaceStore.CreateSpace(ctx, org, groupTp, "search-readable")
	require.NoError(t, err)
	other, err := ts.SpaceStore.CreateSpace(ctx, org, groupTp, "search-other")
	require.NoError(t, err)
	_, err = ts.PermStore.SetUserRelation(ctx, owner, readable, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	readableURI := putIndexed(readable, "k1", "apple pie")
	otherURI := putIndexed(other, "k1", "apple tart")

	client := httpx_testutil.NewTestXRPCClient(t)
	search := func(params url.Values) (int, habitat.NetworkHabitatSpaceSearchRecordsOutput) {
		var out habitat.NetworkHabitatSpaceSearchRecordsOutput
		code := client.Query(ts.Server.SearchRecords, params, &out)
		return code, out
	}

	t.Run("searches every readable space", func(t *testing.T) {
		code, out := search(url.Values{"q": {"apple"}})
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Records, 1)
		rec := out.Records[0]
		require.Equal(t, readableURI, rec.Uri)
		require.Equal(t, readable.String(), rec.Space)
		require.NotEmpty(t, rec.Cid)
		require.Equal(t, map[string]any{"text": "apple pie"}, rec.Value)
		require.Contains(t, rec.Snippet, "<mark>apple</mark>")
	})

	t.Run("searches a space the validator authorized", func(t *testing.T) {
		code, out := search(url.Values{
			"q":          {"apple"},
			"space":      {other.String()},
			"collection": {coll.String()},
			"repo":       {owner.String()},
		})
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Records, 1)
		require.Equal(t, otherURI, out.Records[0].Uri)
	})

	t.Run("filters by collection", func(t *testing.T) {
		code, out := search(url.Values{
			"q":          {"apple"},
			"space":      {other.String()},
			"collection": {"network.habitat.other"},
		})
		require.Equal(t, http.StatusOK, code)
		require.Empty(t, out.Records)
	})

	t.Run("scopes a space credential to its space", func(t *testing.T) {
		// Shares the first server's storage, so it sees the same index.
		credTS := pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithDB(ts.DB),
			pearserver_testutil.WithFGA(ts.FGA),
			pearserver_testutil.WithSpaceStore(ts.SpaceStore),
			pearserver_testutil.WithValidator(authntest.NewSuccessValidator(&authn.CredentialInfo{
				Space:  other,
				Method: authn.ValidatorMethodSpaceCredential,
			})),
		)
		var out habitat.NetworkHabitatSpaceSearchRecordsOutput
		code := client.Query(credTS.Server.SearchRecords, url.Values{"q": {"apple"}}, &out)
		require.Equal(t, http.StatusOK, code)
		require.Len(t, out.Records, 1)
		require.Equal(t, otherURI, out.Records[0].Uri)
	})

	t.Run("rejects bad params", func(t *testing.T) {
		for _, params := range []url.Values{
			{},
			{"q": {"apple"}, "limit": {"101"}},
			{"q": {"apple"}, "collection": {"not an nsid"}},
			{"q": {"apple"}, "space": {"not a space"}},
		} {
			code, _ := search(params)
			require.Equal(t, http.StatusBadRequest, code, params)
		}
	})
}
