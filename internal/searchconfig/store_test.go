package searchconfig_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/search"
	"github.com/habitat-network/habitat/internal/searchconfig"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

func TestStore(t *testing.T) {
	spaces := spaces_testutil.NewTestStore(
		t, spaces_testutil.WithDB(pear_testutil.NewPearDB(t)),
	)
	s := searchconfig.NewStore(spaces)
	orgA := syntax.DID("did:web:a.example.com")
	orgB := syntax.DID("did:web:b.example.com")
	for _, org := range []syntax.DID{orgA, orgB} {
		_, err := spaces.CreateSpace(t.Context(), org, opensocial.MembersSpaceType, "self")
		require.NoError(t, err)
	}

	got, err := s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Empty(t, got)

	require.NoError(t, s.Add(t.Context(), orgA, "com.example.post"))
	require.NoError(t, s.Add(t.Context(), orgA, "com.example.note"))
	// Adding again does nothing.
	require.NoError(t, s.Add(t.Context(), orgA, "com.example.post"))
	require.NoError(t, s.Add(t.Context(), orgB, "com.example.other"))

	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.note", "com.example.post"}, got)
	got, err = s.List(t.Context(), orgB)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.other"}, got)

	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	// Removing one that isn't configured does nothing.
	require.NoError(t, s.Remove(t.Context(), orgA, "com.example.post"))
	got, err = s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Equal(t, []syntax.NSID{"com.example.note"}, got)

	require.Equal(t, searchconfig.DefaultCollections, s.Defaults())
}

func TestStoreConfigs(t *testing.T) {
	store := spaces_testutil.NewTestStore(
		t, spaces_testutil.WithDB(pear_testutil.NewPearDB(t)),
	)
	s := searchconfig.NewStore(store)
	org := syntax.DID("did:web:a.example.com")
	members, err := store.CreateSpace(t.Context(), org, opensocial.MembersSpaceType, "self")
	require.NoError(t, err)
	put := func(collection syntax.NSID, value map[string]any) string {
		t.Helper()
		value["updatedAt"] = "2026-10-14T00:00:00Z"
		_, _, err := store.PutRecord(t.Context(), members, org, searchconfig.Collection,
			syntax.RecordKey(collection), spaces_testutil.MustMarshalRecord(t, value))
		require.NoError(t, err)
		rec, err := store.GetRecord(t.Context(), members, org, searchconfig.Collection,
			syntax.RecordKey(collection))
		require.NoError(t, err)
		return rec.Rev
	}

	// An org with no records gets the defaults.
	got, err := s.Configs(t.Context(), org)
	require.NoError(t, err)
	require.Len(t, got, len(searchconfig.DefaultCollections))
	for _, c := range searchconfig.DefaultCollections {
		require.Equal(t, search.DefaultConfig(), got[c])
	}

	// A record overrides a default, and adds a collection.
	markdown := searchconfig.DefaultCollections[0]
	markdownRev := put(markdown, map[string]any{
		"users": []any{
			map[string]any{
				"$type": "network.habitat.search.config#metadataSource",
				"name":  "owner",
			},
		},
		"date": nil,
		"fields": []any{
			map[string]any{"path": "title", "searchable": "high"},
			map[string]any{"path": "dueAt", "type": "datetime", "sortable": true, "slot": "date_0"},
		},
		"blobs": false,
	})
	postRev := put("com.example.post", map[string]any{})
	// Two fields in one slot: invalid, so it gets the default.
	badRev := put("com.example.bad", map[string]any{"fields": []any{
		map[string]any{"path": "a", "type": "number", "filterable": true, "slot": "num_0"},
		map[string]any{"path": "b", "type": "number", "filterable": true, "slot": "num_0"},
	}})
	got, err = s.Configs(t.Context(), org)
	require.NoError(t, err)
	require.Len(t, got, len(searchconfig.DefaultCollections)+2)
	require.Equal(t, search.CollectionConfig{
		Rev:   markdownRev,
		Users: []search.ValueSource{search.MetadataSource{Name: search.MetadataOwner}},
		Date:  []search.ValueSource{},
		Fields: []search.Field{
			{Path: "title", Searchable: search.WeightHigh},
			{Path: "dueAt", Type: search.FieldDatetime, Sortable: true, Slot: "date_0"},
		},
		NoBlobs: true,
	}, got[markdown])
	require.Equal(t, search.CollectionConfig{Rev: postRev}, got["com.example.post"])
	require.Equal(t, search.CollectionConfig{Rev: badRev}, got["com.example.bad"])
}
