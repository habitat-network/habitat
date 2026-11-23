package spaces_test

import (
	"bytes"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	"github.com/habitat-network/habitat/internal/star"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	importAuthority = syntax.DID("did:plc:authority")
	importAuthor    = syntax.DID("did:plc:author")
	importType      = syntax.NSID("network.habitat.test")
	noteColl        = syntax.NSID("network.habitat.note")
	postColl        = syntax.NSID("network.habitat.post")
)

func newImportSpace(t *testing.T) (spaces.Store, habitat_syntax.SpaceURI) {
	t.Helper()
	store := spaces_testutil.NewTestStore(t)
	uri, err := store.CreateSpace(t.Context(), importAuthority, importType, "main")
	require.NoError(t, err)
	return store, uri
}

func putImport(
	t *testing.T,
	store spaces.Store,
	uri habitat_syntax.SpaceURI,
	coll syntax.NSID,
	rkey syntax.RecordKey,
	value map[string]any,
) {
	t.Helper()
	_, _, err := store.PutRecord(
		t.Context(), uri, importAuthor, coll, rkey, spaces_testutil.MustMarshalRecord(t, value),
	)
	require.NoError(t, err)
}

// archiveOf is the getRepo archive of importAuthor's repo in uri.
func archiveOf(t *testing.T, store spaces.Store, uri habitat_syntax.SpaceURI) []byte {
	t.Helper()
	commit, blocks, err := store.RepoSnapshot(t.Context(), uri, importAuthor)
	require.NoError(t, err)
	require.NotNil(t, commit)
	archive, err := spaces.SerializeRepoStar(uri, importAuthor, *commit, blocks)
	require.NoError(t, err)
	return archive
}

func TestReadRepoStar(t *testing.T) {
	t.Parallel()
	store, uri := newImportSpace(t)
	putImport(t, store, uri, postColl, "b", map[string]any{"text": "second"})
	putImport(t, store, uri, noteColl, "a", map[string]any{"text": "first"})

	archive, err := spaces.ReadRepoStar(bytes.NewReader(archiveOf(t, store, uri)))
	require.NoError(t, err)

	require.Equal(t, importAuthor, archive.Commit.Repo)
	require.Equal(t, uri.Canonical(), archive.Commit.Space.Canonical())
	require.Len(t, archive.Records, 2)
	require.Equal(t, noteColl, archive.Records[0].Collection)
	require.Equal(t, syntax.RecordKey("a"), archive.Records[0].Rkey)
	require.Equal(t, postColl, archive.Records[1].Collection)
	decoded, err := atdata.UnmarshalCBOR(archive.Records[0].Value)
	require.NoError(t, err)
	require.Equal(t, "first", decoded["text"])
}

func TestReadRepoStar_Rejects(t *testing.T) {
	t.Parallel()
	store, uri := newImportSpace(t)
	putImport(t, store, uri, noteColl, "a", map[string]any{"text": "first"})
	putImport(t, store, uri, noteColl, "b", map[string]any{"text": "second"})

	rebuild := func(t *testing.T, meta map[string]any, keys []string, values [][]byte) []byte {
		t.Helper()
		var buf bytes.Buffer
		w, err := star.NewWriter(&buf, meta)
		require.NoError(t, err)
		for i := range keys {
			require.NoError(t, w.WritePair([]byte(keys[i]), values[i]))
		}
		require.NoError(t, w.Close())
		return buf.Bytes()
	}
	good := archiveOf(t, store, uri)
	r, err := star.NewReader(bytes.NewReader(good), 0)
	require.NoError(t, err)
	var keys []string
	var values [][]byte
	for {
		k, v, err := r.Next()
		if err != nil {
			break
		}
		keys = append(keys, string(k))
		values = append(values, v)
	}
	require.Len(t, keys, 2)

	withMeta := func(edit func(m map[string]any)) map[string]any {
		m := map[string]any{}
		for k, v := range r.Meta {
			m[k] = v
		}
		edit(m)
		return m
	}

	tests := []struct {
		name    string
		archive []byte
	}{
		{"not a star archive", []byte("nope")},
		{"wrong profile", rebuild(t,
			withMeta(func(m map[string]any) { m["$type"] = "atproto-space-meta" }), keys, values)},
		{"commit data does not match the records", rebuild(t,
			withMeta(func(m map[string]any) { m["data"] = atdata.Bytes(make([]byte, 32)) }),
			keys, values)},
		{"a record is missing", rebuild(t, r.Meta, keys[:1], values[:1])},
		{"records out of order", rebuild(t, r.Meta,
			[]string{keys[1], keys[0]}, [][]byte{values[1], values[0]})},
		{"invalid record path", rebuild(t, r.Meta, []string{"no-slash", keys[1]}, values)},
		{"value is not a record", rebuild(t, r.Meta, keys, [][]byte{{0xff}, values[1]})},
		{"unsupported version", rebuild(t,
			withMeta(func(m map[string]any) { m["ver"] = int64(2) }), keys, values)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := spaces.ReadRepoStar(bytes.NewReader(tt.archive))
			require.ErrorIs(t, err, spaces.ErrInvalidRepoArchive)
		})
	}
}

func TestClearRepo(t *testing.T) {
	t.Parallel()
	store, uri := newImportSpace(t)
	putImport(t, store, uri, noteColl, "a", map[string]any{"text": "first"})
	putImport(t, store, uri, postColl, "b", map[string]any{"text": "second"})
	putImport(t, store, uri, habitat_syntax.SpaceRelationCollection, "rel",
		map[string]any{"role": "reader"})

	require.NoError(t, spaces.ClearRepo(t.Context(), store, uri, importAuthor))

	left, err := store.ListRecords(t.Context(), uri, importAuthor, nil)
	require.NoError(t, err)
	require.Len(t, left, 1, "reserved collections are the space's configuration")
	require.Equal(t, habitat_syntax.SpaceRelationCollection, left[0].Collection)

	t.Run("is a no-op when nothing is left", func(t *testing.T) {
		require.NoError(t, spaces.ClearRepo(t.Context(), store, uri, importAuthor))
	})
	t.Run("leaves another repo alone", func(t *testing.T) {
		_, _, err := store.PutRecord(t.Context(), uri, "did:plc:other", noteColl, "x",
			spaces_testutil.MustMarshalRecord(t, map[string]any{"n": 1}))
		require.NoError(t, err)
		require.NoError(t, spaces.ClearRepo(t.Context(), store, uri, importAuthor))
		others, err := store.ListRecords(t.Context(), uri, "did:plc:other", nil)
		require.NoError(t, err)
		require.Len(t, others, 1)
	})
	t.Run("a missing space has nothing to clear", func(t *testing.T) {
		missing := habitat_syntax.SpaceURI("at://did:plc:authority/space/network.habitat.test/gone")
		err := spaces.ClearRepo(t.Context(), store, missing, importAuthor)
		require.NoError(t, err)
	})
}

func TestImportRepo(t *testing.T) {
	t.Parallel()

	// source holds the repo being migrated; target is where it lands.
	source, sourceURI := newImportSpace(t)
	putImport(t, source, sourceURI, noteColl, "keep", map[string]any{"text": "new value"})
	putImport(t, source, sourceURI, noteColl, "added", map[string]any{"text": "added"})
	archive, err := spaces.ReadRepoStar(bytes.NewReader(archiveOf(t, source, sourceURI)))
	require.NoError(t, err)

	target, targetURI := newImportSpace(t)
	putImport(t, target, targetURI, noteColl, "keep", map[string]any{"text": "old value"})
	putImport(t, target, targetURI, postColl, "stale", map[string]any{"text": "gone"})

	require.NoError(t, spaces.ImportRepo(
		t.Context(), target, targetURI, importAuthor, archive.Records,
	))

	records, err := target.ListRecords(t.Context(), targetURI, importAuthor, nil)
	require.NoError(t, err)
	got := map[string]any{}
	for _, rec := range records {
		got[rec.Collection.String()+"/"+rec.Rkey.String()] = rec.Value["text"]
	}
	require.Equal(t, map[string]any{
		"network.habitat.note/keep":  "new value",
		"network.habitat.note/added": "added",
	}, got)

	// The repo now commits to exactly the archive's records.
	commit, _, err := target.RepoSnapshot(t.Context(), targetURI, importAuthor)
	require.NoError(t, err)
	require.Equal(t, archive.Commit.Data, commit.Data)

	t.Run("importing again changes nothing", func(t *testing.T) {
		require.NoError(t, spaces.ImportRepo(
			t.Context(), target, targetURI, importAuthor, archive.Records,
		))
		again, _, err := target.RepoSnapshot(t.Context(), targetURI, importAuthor)
		require.NoError(t, err)
		require.Equal(t, commit.Rev, again.Rev)
	})
	t.Run("a missing space must be imported first", func(t *testing.T) {
		missing := habitat_syntax.SpaceURI("at://did:plc:authority/space/network.habitat.test/gone")
		err := spaces.ImportRepo(t.Context(), target, missing, importAuthor, archive.Records)
		require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
		err = spaces.ImportRepo(t.Context(), target, missing, importAuthor, nil)
		require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
	})
	t.Run("refuses reserved collections", func(t *testing.T) {
		reserved := []spaces.ImportedRecord{{
			Collection: habitat_syntax.UserRelationCollection, Rkey: "r",
			Value: spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
		}}
		err := spaces.ImportRepo(t.Context(), target, targetURI, importAuthor, reserved)
		require.ErrorIs(t, err, spaces.ErrReservedCollection)
	})
}

func TestImportWriters(t *testing.T) {
	t.Parallel()
	store, uri := newImportSpace(t)
	// A writer this host already holds records for is left as it is.
	putImport(t, store, uri, noteColl, "a", map[string]any{"text": "mine"})
	before, err := store.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, before, 1)

	future := syntax.TID("3zzzzzzzzzzz2")
	digest := bytes.Repeat([]byte{7}, 32)
	require.NoError(t, store.ImportWriters(t.Context(), uri, []spaces.MetaWriter{
		{DID: importAuthor, RepoRev: "3kabcdefghij2", Data: bytes.Repeat([]byte{9}, 32),
			SpaceRev: "3kabcdefghij3"},
		{DID: "did:plc:remote", RepoRev: "3kabcdefghij4", Data: digest, SpaceRev: future},
	}))

	repos, err := store.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, repos, 2)
	byDID := map[syntax.DID]spaces.RepoInfo{}
	for _, r := range repos {
		byDID[r.DID] = r
	}
	require.Equal(t, before[0], byDID[importAuthor], "existing writer is untouched")
	remote := byDID["did:plc:remote"]
	require.Equal(t, "3kabcdefghij4", remote.Rev)
	require.Equal(t, digest, remote.Hash)
	require.Equal(t, future, remote.SpaceRev)

	rev, err := store.SpaceRev(t.Context(), uri)
	require.NoError(t, err)
	require.Equal(t, future, rev, "the space revision moves up to the writers'")

	t.Run("a missing space is not found", func(t *testing.T) {
		missing := habitat_syntax.SpaceURI("at://did:plc:authority/space/network.habitat.test/gone")
		err := store.ImportWriters(t.Context(), missing, nil)
		require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
	})
}
