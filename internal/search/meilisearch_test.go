package search_test

import (
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/search/searchtest"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	spaceA = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.test/a")
	spaceB = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.test/b")
	alice  = syntax.DID("did:plc:alice")
)

// aliceReads lets alice read a document.
var aliceReads = search.Access{Principals: search.Principals{Users: []syntax.DID{alice}}}

// asAlice runs q as alice.
func asAlice(q search.Query) search.Query {
	q.Reader = &search.Reader{Users: []syntax.DID{alice}}
	return q
}

func doc(space habitat_syntax.SpaceURI, rkey, rev, text string) search.Document {
	return search.Document{
		URI: habitat_syntax.SpaceRecordURI(
			fmt.Sprintf("%s/did:plc:user/network.habitat.note/%s", space, rkey),
		),
		Space:      space,
		Repo:       "did:plc:user",
		Collection: "network.habitat.note",
		Rev:        syntax.TID(rev),
		Text:       text,
		Access:     aliceReads,
	}
}

func search_(t *testing.T, idx search.Index, q search.Query) search.Result {
	t.Helper()
	res, err := idx.Search(t.Context(), q)
	require.NoError(t, err)
	return res
}

func uris(res search.Result) []habitat_syntax.SpaceRecordURI {
	out := []habitat_syntax.SpaceRecordURI{}
	for _, h := range res.Hits {
		out = append(out, h.URI)
	}
	return out
}

func TestMeilisearchMatchesEveryWord(t *testing.T) {
	idx := searchtest.NewIndex(t)
	require.NoError(t, idx.Put(t.Context(),
		doc(spaceA, "1", "3kaaaaaaaaaa2", "the quarterly budget review"),
		doc(spaceA, "2", "3kaaaaaaaaaa2", "budget planning"),
		doc(spaceA, "3", "3kaaaaaaaaaa2", "grocery list"),
	))
	res := search_(t, idx, asAlice(search.Query{Text: "budget"}))
	require.ElementsMatch(t, []habitat_syntax.SpaceRecordURI{
		doc(spaceA, "1", "", "").URI, doc(spaceA, "2", "", "").URI,
	}, uris(res))
	require.Empty(t, res.Cursor)

	res = search_(t, idx, asAlice(search.Query{Text: "budget review"}))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{doc(spaceA, "1", "", "").URI}, uris(res))
	hit := res.Hits[0]
	require.Equal(t, spaceA, hit.Space)
	require.Equal(t, syntax.DID("did:plc:user"), hit.Repo)
	require.Equal(t, syntax.NSID("network.habitat.note"), hit.Collection)
	require.Contains(t, hit.Snippet, "<mark>budget</mark>")
}

func TestMeilisearchFiltersByAccess(t *testing.T) {
	idx := searchtest.NewIndex(t)
	ctx := t.Context()
	byUser := doc(spaceA, "user", "3kaaaaaaaaaa2", "secret plans")
	byCommunityRole := doc(spaceA, "community", "3kaaaaaaaaaa2", "secret plans")
	byCommunityRole.Access = search.Access{Principals: search.Principals{
		CommunityRoles: []search.CommunityRole{{Community: "did:plc:org", Role: "staff"}},
	}}
	public := doc(spaceA, "public", "3kaaaaaaaaaa2", "secret plans")
	public.Access = search.Access{Public: true}
	require.NoError(t, idx.Put(ctx, byUser, byCommunityRole, public))

	for _, tc := range []struct {
		name   string
		reader search.Reader
		want   []search.Document
	}{
		{"nobody", search.Reader{}, []search.Document{public}},
		{"user", search.Reader{Users: []syntax.DID{alice}}, []search.Document{byUser, public}},
		{"other user", search.Reader{Users: []syntax.DID{"did:plc:bob"}}, []search.Document{public}},
		{"community role", search.Reader{
			CommunityRoles: []search.CommunityRole{{Community: "did:plc:org", Role: "staff"}},
		}, []search.Document{byCommunityRole, public}},
		{"other community's role", search.Reader{
			CommunityRoles: []search.CommunityRole{{Community: "did:plc:other", Role: "staff"}},
		}, []search.Document{public}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := search_(t, idx, search.Query{Text: "secret", Reader: &tc.reader})
			want := []habitat_syntax.SpaceRecordURI{}
			for _, d := range tc.want {
				want = append(want, d.URI)
			}
			require.ElementsMatch(t, want, uris(res))
		})
	}

	// Without a reader, a query must name the spaces it searches.
	require.Empty(t, search_(t, idx, search.Query{Text: "secret"}).Hits)
	res := search_(t, idx, search.Query{Text: "secret", Spaces: []habitat_syntax.SpaceURI{spaceA}})
	require.Len(t, res.Hits, 3)
}

func TestMeilisearchSetSpaceAccess(t *testing.T) {
	idx := searchtest.NewIndex(t)
	ctx := t.Context()
	require.NoError(t, idx.Put(ctx,
		doc(spaceA, "1", "3kaaaaaaaaaa2", "apples"),
		doc(spaceB, "1", "3kaaaaaaaaaa2", "apples"),
	))
	bob := search.Access{Principals: search.Principals{Users: []syntax.DID{"did:plc:bob"}}}
	require.NoError(t, idx.SetSpaceAccess(ctx, spaceA, bob))

	res := search_(t, idx, asAlice(search.Query{Text: "apples"}))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{doc(spaceB, "1", "", "").URI}, uris(res))
	res = search_(t, idx, search.Query{
		Text: "apples", Reader: &search.Reader{Users: []syntax.DID{"did:plc:bob"}},
	})
	require.Equal(t, []habitat_syntax.SpaceRecordURI{doc(spaceA, "1", "", "").URI}, uris(res))
}

func TestMeilisearchFiltersBySpaceCollectionAndRepo(t *testing.T) {
	idx := searchtest.NewIndex(t)
	other := doc(spaceA, "2", "3kaaaaaaaaaa2", "note about cats")
	other.Collection = "network.habitat.other"
	other.Repo = "did:plc:someone"
	inB := doc(spaceB, "1", "3kaaaaaaaaaa2", "note about cats")
	require.NoError(t, idx.Put(t.Context(),
		doc(spaceA, "1", "3kaaaaaaaaaa2", "note about cats"), other, inB))

	res := search_(t, idx, asAlice(search.Query{
		Text: "cats", Spaces: []habitat_syntax.SpaceURI{spaceB},
	}))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{inB.URI}, uris(res))

	res = search_(t, idx, asAlice(search.Query{
		Text: "cats", Collections: []syntax.NSID{"network.habitat.other"},
	}))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{other.URI}, uris(res))

	res = search_(t, idx, asAlice(search.Query{
		Text: "cats", Repos: []syntax.DID{"did:plc:someone"},
	}))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{other.URI}, uris(res))
}

func TestMeilisearchKeepsNewestRevision(t *testing.T) {
	idx := searchtest.NewIndex(t)
	ctx := t.Context()
	require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa3", "apples")))
	require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa2", "oranges")))
	require.Empty(t, search_(t, idx, asAlice(search.Query{Text: "oranges"})).Hits,
		"an older revision must not replace a newer one")

	require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa4", "pears")))
	require.Empty(t, search_(t, idx, asAlice(search.Query{Text: "apples"})).Hits)
	require.Len(t, search_(t, idx, asAlice(search.Query{Text: "pears"})).Hits, 1)

	// Within one Put, too.
	require.NoError(t, idx.Put(ctx,
		doc(spaceA, "2", "3kaaaaaaaaaa3", "newer"),
		doc(spaceA, "2", "3kaaaaaaaaaa2", "older"),
	))
	require.Len(t, search_(t, idx, asAlice(search.Query{Text: "newer"})).Hits, 1)
	require.Empty(t, search_(t, idx, asAlice(search.Query{Text: "older"})).Hits)
}

func TestMeilisearchDeletes(t *testing.T) {
	idx := searchtest.NewIndex(t)
	ctx := t.Context()
	require.NoError(t, idx.Put(ctx,
		doc(spaceA, "1", "3kaaaaaaaaaa2", "apples"),
		doc(spaceA, "2", "3kaaaaaaaaaa2", "apples"),
		doc(spaceB, "1", "3kaaaaaaaaaa2", "apples"),
	))
	require.NoError(t, idx.Delete(ctx, doc(spaceA, "1", "", "").URI, "at://missing"))
	require.Len(t, search_(t, idx, asAlice(search.Query{Text: "apples"})).Hits, 2)

	require.NoError(t, idx.DeleteSpace(ctx, spaceA))
	res := search_(t, idx, asAlice(search.Query{Text: "apples"}))
	require.Equal(t, []habitat_syntax.SpaceRecordURI{doc(spaceB, "1", "", "").URI}, uris(res))
}

func TestMeilisearchPages(t *testing.T) {
	idx := searchtest.NewIndex(t)
	ctx := t.Context()
	var docs []search.Document
	for i := range 5 {
		docs = append(docs, doc(spaceA, fmt.Sprint(i), "3kaaaaaaaaaa2", "common word"))
	}
	require.NoError(t, idx.Put(ctx, docs...))

	q := asAlice(search.Query{Text: "common", Limit: 2})
	seen := map[habitat_syntax.SpaceRecordURI]bool{}
	for pages := 0; ; pages++ {
		require.Less(t, pages, 5)
		res := search_(t, idx, q)
		for _, h := range res.Hits {
			require.False(t, seen[h.URI], "hit repeated across pages")
			seen[h.URI] = true
		}
		if res.Cursor == "" {
			break
		}
		q.Cursor = res.Cursor
	}
	require.Len(t, seen, 5)

	q.Cursor = "junk"
	_, err := idx.Search(ctx, q)
	require.ErrorIs(t, err, search.ErrInvalidCursor)
}

func TestMeilisearchTreatsInputAsPlainText(t *testing.T) {
	idx := searchtest.NewIndex(t)
	require.NoError(t, idx.Put(t.Context(), doc(spaceA, "1", "3kaaaaaaaaaa2", "cats and dogs")))
	for _, text := range []string{
		`cats "`, `cats OR`, `NOT`, `(cats`, `cats*`, `col:cats`, `-cats`, ``, `   `, `!!!`,
	} {
		_, err := idx.Search(t.Context(), asAlice(search.Query{Text: text}))
		require.NoError(t, err, "query %q", text)
	}
	require.Len(t, search_(t, idx, asAlice(search.Query{Text: `"cats"`})).Hits, 1)
	// Filter values are quoted, so they can't change the filter.
	res := search_(t, idx, search.Query{
		Text:   "cats",
		Reader: &search.Reader{Users: []syntax.DID{`x" OR public = false OR user_readers = "x`}},
	})
	require.Empty(t, res.Hits)
}
