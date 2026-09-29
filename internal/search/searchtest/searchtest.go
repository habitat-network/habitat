// Package searchtest holds the conformance tests every [search.Index]
// implementation must pass.
package searchtest

import (
	"fmt"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/search"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	spaceA = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.test/a")
	spaceB = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.test/b")
)

// in searches one space, with the test's context.
func in(t *testing.T, idx search.Index, text string, space habitat_syntax.SpaceURI) search.Result {
	t.Helper()
	res, err := idx.Search(t.Context(), search.Query{
		Text: text, Spaces: []habitat_syntax.SpaceURI{space},
	})
	require.NoError(t, err)
	return res
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
	}
}

// Run runs the behavior every [search.Index] implementation must have, each
// subtest on a fresh index from newIndex.
func Run(t *testing.T, newIndex func(t *testing.T) search.Index) {
	ctx := t.Context()

	t.Run("matches words and ranks", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx,
			doc(spaceA, "1", "3kaaaaaaaaaa2", "the quarterly budget review"),
			doc(spaceA, "2", "3kaaaaaaaaaa2", "grocery list"),
			doc(spaceA, "3", "3kaaaaaaaaaa2", "budget budget budget planning"),
		))
		res := in(t, idx, "budget", spaceA)
		require.Len(t, res.Hits, 2)
		require.Equal(t, doc(spaceA, "3", "", "").URI, res.Hits[0].URI, "denser match ranks first")
		require.Contains(t, res.Hits[0].Snippet, "<mark>budget</mark>")
		require.Empty(t, res.Cursor)
	})

	t.Run("requires every word", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx,
			doc(spaceA, "1", "3kaaaaaaaaaa2", "budget review"),
			doc(spaceA, "2", "3kaaaaaaaaaa2", "budget planning"),
		))
		res := in(t, idx, "budget review", spaceA)
		require.Len(t, res.Hits, 1)
	})

	t.Run("never returns other spaces", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx,
			doc(spaceA, "1", "3kaaaaaaaaaa2", "secret plans"),
			doc(spaceB, "1", "3kaaaaaaaaaa2", "secret plans"),
		))
		res := in(t, idx, "secret", spaceB)
		require.Len(t, res.Hits, 1)
		require.Equal(t, spaceB, res.Hits[0].Space)

		// No spaces means no results, never an unscoped search.
		res, err := idx.Search(ctx, search.Query{Text: "secret"})
		require.NoError(t, err)
		require.Empty(t, res.Hits)
	})

	t.Run("filters by collection and repo", func(t *testing.T) {
		idx := newIndex(t)
		other := doc(spaceA, "2", "3kaaaaaaaaaa2", "note about cats")
		other.Collection = "network.habitat.other"
		other.Repo = "did:plc:someone"
		require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa2", "note about cats"), other))
		spaces := []habitat_syntax.SpaceURI{spaceA}

		res, err := idx.Search(ctx, search.Query{
			Text: "cats", Spaces: spaces, Collections: []syntax.NSID{"network.habitat.other"},
		})
		require.NoError(t, err)
		require.Len(t, res.Hits, 1)
		require.Equal(t, other.URI, res.Hits[0].URI)

		res, err = idx.Search(ctx, search.Query{
			Text: "cats", Spaces: spaces, Repos: []syntax.DID{"did:plc:user"},
		})
		require.NoError(t, err)
		require.Len(t, res.Hits, 1)
		require.Equal(t, doc(spaceA, "1", "", "").URI, res.Hits[0].URI)
	})

	t.Run("updates and ignores stale revisions", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa3", "apples")))
		require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa2", "oranges")))
		spaces := []habitat_syntax.SpaceURI{spaceA}

		res, err := idx.Search(ctx, search.Query{Text: "oranges", Spaces: spaces})
		require.NoError(t, err)
		require.Empty(t, res.Hits, "an older revision must not replace a newer one")

		require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa4", "pears")))
		res, err = idx.Search(ctx, search.Query{Text: "apples", Spaces: spaces})
		require.NoError(t, err)
		require.Empty(t, res.Hits, "the old text is gone from the index")
		res, err = idx.Search(ctx, search.Query{Text: "pears", Spaces: spaces})
		require.NoError(t, err)
		require.Len(t, res.Hits, 1)
	})

	t.Run("keeps the newest of duplicates in one Put", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx,
			doc(spaceA, "1", "3kaaaaaaaaaa3", "newer"),
			doc(spaceA, "1", "3kaaaaaaaaaa2", "older"),
		))
		res := in(t, idx, "newer", spaceA)
		require.Len(t, res.Hits, 1)
	})

	t.Run("deletes documents and spaces", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx,
			doc(spaceA, "1", "3kaaaaaaaaaa2", "apples"),
			doc(spaceA, "2", "3kaaaaaaaaaa2", "apples"),
			doc(spaceB, "1", "3kaaaaaaaaaa2", "apples"),
		))
		both := []habitat_syntax.SpaceURI{spaceA, spaceB}

		require.NoError(t, idx.Delete(ctx, doc(spaceA, "1", "", "").URI, "at://missing"))
		res, err := idx.Search(ctx, search.Query{Text: "apples", Spaces: both})
		require.NoError(t, err)
		require.Len(t, res.Hits, 2)

		require.NoError(t, idx.DeleteSpace(ctx, spaceA))
		res, err = idx.Search(ctx, search.Query{Text: "apples", Spaces: both})
		require.NoError(t, err)
		require.Len(t, res.Hits, 1)
		require.Equal(t, spaceB, res.Hits[0].Space)
	})

	t.Run("pages results", func(t *testing.T) {
		idx := newIndex(t)
		var docs []search.Document
		for i := range 5 {
			docs = append(docs, doc(spaceA, fmt.Sprint(i), "3kaaaaaaaaaa2", "common word"))
		}
		require.NoError(t, idx.Put(ctx, docs...))

		q := search.Query{Text: "common", Spaces: []habitat_syntax.SpaceURI{spaceA}, Limit: 2}
		seen := map[habitat_syntax.SpaceRecordURI]bool{}
		for pages := 0; ; pages++ {
			require.Less(t, pages, 5)
			res, err := idx.Search(ctx, q)
			require.NoError(t, err)
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

		_, err := idx.Search(ctx, search.Query{Text: "common", Spaces: q.Spaces, Cursor: "junk"})
		require.Error(t, err)
	})

	t.Run("treats input as plain text", func(t *testing.T) {
		idx := newIndex(t)
		require.NoError(t, idx.Put(ctx, doc(spaceA, "1", "3kaaaaaaaaaa2", "cats and dogs")))
		spaces := []habitat_syntax.SpaceURI{spaceA}
		hostile := []string{
			`cats "`, `cats OR`, `NOT`, `(cats`, `cats*`, `col:cats`, `-cats`, ``, `   `, `!!!`,
		}
		for _, text := range hostile {
			_, err := idx.Search(ctx, search.Query{Text: text, Spaces: spaces})
			require.NoError(t, err, "query %q", text)
		}
		res, err := idx.Search(ctx, search.Query{Text: `"cats"`, Spaces: spaces})
		require.NoError(t, err)
		require.Len(t, res.Hits, 1)
	})
}
