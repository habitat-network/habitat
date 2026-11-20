package spaces_test

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	noteCollection = syntax.NSID("network.habitat.note")
	likeCollection = syntax.NSID("network.habitat.feed.like")
)

// listAll sequences and returns every event in the log matching filter.
func listAll(t *testing.T, s spaces.Store, filter spaces.EventFilter) []spaces.Event {
	t.Helper()
	_, err := s.SequenceEvents(t.Context())
	require.NoError(t, err)
	events, err := s.ListEvents(t.Context(), filter, 0, 1000)
	require.NoError(t, err)
	return events
}

// commitKeys summarizes commit events as "<repo> <collection>/<rkey> <op>".
func commitKeys(t *testing.T, events []spaces.Event) []string {
	t.Helper()
	keys := make([]string, 0, len(events))
	for _, e := range events {
		switch e := e.(type) {
		case spaces.CommitEvent:
			keys = append(keys, string(e.Repo)+" "+string(e.Collection)+"/"+
				string(e.Rkey)+" "+string(e.Operation))
		case spaces.SpaceDeletedEvent:
			keys = append(keys, "deleted "+string(e.Space))
		}
	}
	return keys
}

func putNote(
	t *testing.T,
	s spaces.Store,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
	text string,
) {
	t.Helper()
	_, _, err := s.PutRecord(t.Context(), space, repo, collection, rkey,
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": text}))
	require.NoError(t, err)
}

func TestEventsLogFollowsWrites(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	ctx := t.Context()
	space, err := s.CreateSpace(ctx, orgID, groupType, "events")
	require.NoError(t, err)

	head, err := s.SequenceEvents(ctx)
	require.NoError(t, err)
	require.Zero(t, head, "an empty log has head 0")

	putNote(t, s, space, alice, noteCollection, "a", "one")
	putNote(t, s, space, owner, noteCollection, "b", "two")

	// Writes are pending until sequenced.
	events, err := s.ListEvents(ctx, spaces.EventFilter{}, 0, 100)
	require.NoError(t, err)
	require.Empty(t, events)

	head, err = s.SequenceEvents(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 2, head)
	again, err := s.SequenceEvents(ctx)
	require.NoError(t, err)
	require.Equal(t, head, again, "sequencing with nothing pending leaves the head")

	events, err = s.ListEvents(ctx, spaces.EventFilter{}, 0, 100)
	require.NoError(t, err)
	require.Len(t, events, 2)
	first, ok := events[0].(spaces.CommitEvent)
	require.True(t, ok)
	require.Equal(t, space, first.Space)
	require.Equal(t, alice, first.Repo)
	require.Equal(t, noteCollection, first.Collection)
	require.Equal(t, syntax.RecordKey("a"), first.Rkey)
	require.Equal(t, spaces.WriteCreate, first.Operation)
	require.Equal(t, "one", first.Value["text"])
	require.True(t, first.Cid.Defined())
	require.NotEmpty(t, first.Rev)
	require.False(t, first.Time.IsZero())
	require.Less(t, first.Seq, events[1].EventSeq())

	// An update moves the record to the end of the log, and a delete does the
	// same: each record appears once, at its latest write.
	putNote(t, s, space, alice, noteCollection, "a", "one, edited")
	require.NoError(t, s.DeleteRecord(ctx, space, owner, noteCollection, "b"))
	events = listAll(t, s, spaces.EventFilter{})
	require.Equal(t, []string{
		"did:plc:alice network.habitat.note/a update",
		"did:plc:owner network.habitat.note/b delete",
	}, commitKeys(t, events))
	updated, ok := events[0].(spaces.CommitEvent)
	require.True(t, ok)
	require.Equal(t, "one, edited", updated.Value["text"])
	deleted, ok := events[1].(spaces.CommitEvent)
	require.True(t, ok)
	require.Nil(t, deleted.Value)
	require.False(t, deleted.Cid.Defined())
	require.Greater(t, updated.Seq, head, "a rewritten record takes a new seq")
}

func TestEventsApplyWritesInOrder(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	ctx := t.Context()
	space, err := s.CreateSpace(ctx, orgID, groupType, "batch")
	require.NoError(t, err)

	value := spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "x"})
	_, err = s.ApplyWrites(ctx, space, alice, []spaces.Write{
		{Action: spaces.WriteCreate, Collection: noteCollection, Rkey: "1", Value: value},
		{Action: spaces.WriteCreate, Collection: noteCollection, Rkey: "2", Value: value},
		{Action: spaces.WriteCreate, Collection: noteCollection, Rkey: "3", Value: value},
	})
	require.NoError(t, err)
	_, err = s.ApplyWrites(ctx, space, alice, []spaces.Write{
		{Action: spaces.WriteDelete, Collection: noteCollection, Rkey: "1"},
	})
	require.NoError(t, err)

	require.Equal(t, []string{
		"did:plc:alice network.habitat.note/2 create",
		"did:plc:alice network.habitat.note/3 create",
		"did:plc:alice network.habitat.note/1 delete",
	}, commitKeys(t, listAll(t, s, spaces.EventFilter{})))
}

func TestEventsSpaceDeleted(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	ctx := t.Context()
	space, err := s.CreateSpace(ctx, orgID, groupType, "doomed")
	require.NoError(t, err)
	putNote(t, s, space, alice, noteCollection, "a", "one")
	_, err = s.SequenceEvents(ctx)
	require.NoError(t, err)

	require.NoError(t, s.DeleteSpace(ctx, space))
	events := listAll(t, s, spaces.EventFilter{})
	require.Len(t, events, 2)
	deleted, ok := events[1].(spaces.SpaceDeletedEvent)
	require.True(t, ok)
	require.Equal(t, space, deleted.Space)
	require.Greater(t, deleted.Seq, events[0].EventSeq())
	require.False(t, deleted.Time.IsZero())

	// Recreating the space retires its deletion: the new space isn't deleted.
	_, err = s.CreateSpace(ctx, orgID, groupType, "doomed")
	require.NoError(t, err)
	require.Len(t, listAll(t, s, spaces.EventFilter{}), 1)
}

func TestEventsCursorAndLimit(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	ctx := t.Context()
	space, err := s.CreateSpace(ctx, orgID, groupType, "paged")
	require.NoError(t, err)
	for _, rkey := range []syntax.RecordKey{"1", "2", "3", "4", "5"} {
		putNote(t, s, space, alice, noteCollection, rkey, "x")
	}
	all := listAll(t, s, spaces.EventFilter{})
	require.Len(t, all, 5)

	page, err := s.ListEvents(ctx, spaces.EventFilter{}, all[1].EventSeq(), 2)
	require.NoError(t, err)
	require.Equal(t, all[1:3], page, "the cursor is inclusive")

	page, err = s.ListEvents(ctx, spaces.EventFilter{}, all[4].EventSeq()+1, 2)
	require.NoError(t, err)
	require.Empty(t, page)
}

func TestEventsFilter(t *testing.T) {
	s := spaces_testutil.NewTestStore(t)
	ctx := t.Context()
	spaceA, err := s.CreateSpace(ctx, orgID, groupType, "a")
	require.NoError(t, err)
	spaceB, err := s.CreateSpace(ctx, orgID, groupType, "b")
	require.NoError(t, err)
	putNote(t, s, spaceA, alice, noteCollection, "1", "x")
	putNote(t, s, spaceA, owner, likeCollection, "2", "x")
	putNote(t, s, spaceB, alice, likeCollection, "3", "x")
	require.NoError(t, s.DeleteSpace(ctx, spaceB))

	tests := []struct {
		name   string
		filter spaces.EventFilter
		want   []string
	}{
		{
			name: "match all",
			want: []string{
				"did:plc:alice network.habitat.note/1 create",
				"did:plc:owner network.habitat.feed.like/2 create",
				// spaceB's record was pending when the space was deleted, so
				// it is sequenced as deleted.
				"did:plc:alice network.habitat.feed.like/3 delete",
				"deleted " + string(spaceB),
			},
		},
		{
			name:   "space",
			filter: spaces.EventFilter{Spaces: []habitat_syntax.SpaceURI{spaceA}},
			want: []string{
				"did:plc:alice network.habitat.note/1 create",
				"did:plc:owner network.habitat.feed.like/2 create",
			},
		},
		{
			name:   "repo leaves space deletions",
			filter: spaces.EventFilter{Repos: []syntax.DID{owner}},
			want: []string{
				"did:plc:owner network.habitat.feed.like/2 create",
				"deleted " + string(spaceB),
			},
		},
		{
			name: "collection",
			filter: spaces.EventFilter{
				Spaces:      []habitat_syntax.SpaceURI{spaceA},
				Collections: []syntax.NSID{noteCollection},
			},
			want: []string{"did:plc:alice network.habitat.note/1 create"},
		},
		{
			name: "collection prefix",
			filter: spaces.EventFilter{
				Spaces:             []habitat_syntax.SpaceURI{spaceA, spaceB},
				CollectionPrefixes: []string{"network.habitat.feed."},
			},
			want: []string{
				"did:plc:owner network.habitat.feed.like/2 create",
				"did:plc:alice network.habitat.feed.like/3 delete",
				"deleted " + string(spaceB),
			},
		},
		{
			name: "collection or prefix",
			filter: spaces.EventFilter{
				Spaces:             []habitat_syntax.SpaceURI{spaceA},
				Collections:        []syntax.NSID{noteCollection},
				CollectionPrefixes: []string{"network.habitat.feed."},
			},
			want: []string{
				"did:plc:alice network.habitat.note/1 create",
				"did:plc:owner network.habitat.feed.like/2 create",
			},
		},
		{
			name:   "prefix wildcards are literal",
			filter: spaces.EventFilter{CollectionPrefixes: []string{"network.%."}},
			want:   []string{"deleted " + string(spaceB)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, commitKeys(t, listAll(t, s, tt.filter)))
		})
	}
}

// remoteNote is a fetched op putting a note, or deleting it when text is
// empty.
func remoteNote(t *testing.T, rev syntax.TID, rkey syntax.RecordKey, text string) spaces.RemoteOp {
	t.Helper()
	op := spaces.RemoteOp{Rev: rev, Collection: noteCollection, Rkey: rkey}
	if text == "" {
		return op
	}
	record := spaces_testutil.MustMarshalRecord(t, map[string]any{"text": text})
	c, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(record)
	require.NoError(t, err)
	op.Value = record
	op.Cid = c.String()
	return op
}

func TestEventsIncludeExternalWritersRecords(t *testing.T) {
	testExternalWritersRecords(t, spaces_testutil.NewTestStore(t))
}

// testExternalWritersRecords checks that records fetched from an external
// writer's PDS are carried by s's event log.
func testExternalWritersRecords(t *testing.T, s spaces.Store) {
	ctx := t.Context()
	space, err := s.CreateSpace(ctx, orgID, groupType, "external")
	require.NoError(t, err)
	external := syntax.DID("did:plc:external")
	clock := syntax.NewTIDClock(0)
	rev1, rev2, rev3 := clock.Next(), clock.Next(), clock.Next()

	// A local writer isn't an external one.
	putNote(t, s, space, alice, noteCollection, "a", "local")
	_, err = s.GetRemoteRepo(ctx, space, alice)
	require.ErrorIs(t, err, spaces.ErrRepoNotRemote)

	require.NoError(t, s.RegisterRemoteWrite(ctx, space, external, rev2, []byte("digest")))
	behind, err := s.ListRemoteReposBehind(ctx)
	require.NoError(t, err)
	require.Len(t, behind, 1)
	require.Equal(t, external, behind[0].Repo)
	require.Empty(t, behind[0].SyncedRev)
	require.Equal(t, rev2, behind[0].ReportedRev)

	require.NoError(t, s.ApplyRemoteSync(ctx, space, external, spaces.RemoteSync{
		Ops: []spaces.RemoteOp{
			remoteNote(t, rev1, "x", "one"),
			remoteNote(t, rev2, "y", "two"),
		},
		Rev:     rev2,
		Rebuild: true,
	}))
	behind, err = s.ListRemoteReposBehind(ctx)
	require.NoError(t, err)
	require.Empty(t, behind)
	require.Equal(t, []string{
		"did:plc:alice network.habitat.note/a create",
		"did:plc:external network.habitat.note/x create",
		"did:plc:external network.habitat.note/y create",
	}, commitKeys(t, listAll(t, s, spaces.EventFilter{})))
	events := listAll(t, s, spaces.EventFilter{Repos: []syntax.DID{external}})
	first, ok := events[0].(spaces.CommitEvent)
	require.True(t, ok)
	require.Equal(t, "one", first.Value["text"])
	require.True(t, first.Cid.Defined())
	require.Equal(t, space, first.Space)

	// Later ops replace the records they name, compacted like local ones,
	// and the collection filter applies to them.
	require.NoError(t, s.RegisterRemoteWrite(ctx, space, external, rev3, []byte("digest 2")))
	update := remoteNote(t, rev3, "x", "one, edited")
	update.Prev = first.Cid.String()
	require.NoError(t, s.ApplyRemoteSync(ctx, space, external, spaces.RemoteSync{
		Ops: []spaces.RemoteOp{update, remoteNote(t, rev3, "y", "")},
		Rev: rev3,
	}))
	require.Equal(t, []string{
		"did:plc:external network.habitat.note/x update",
		"did:plc:external network.habitat.note/y delete",
	}, commitKeys(t, listAll(t, s, spaces.EventFilter{Repos: []syntax.DID{external}})))
	require.Empty(t, listAll(t, s, spaces.EventFilter{
		Repos:       []syntax.DID{external},
		Collections: []syntax.NSID{likeCollection},
	}))
	remote, err := s.GetRemoteRepo(ctx, space, external)
	require.NoError(t, err)
	require.Equal(t, rev3, remote.SyncedRev)
	require.False(t, remote.Behind())

	// Deleting the space drops the fetched records with it.
	require.NoError(t, s.DeleteSpace(ctx, space))
	require.Equal(t, []string{
		"did:plc:alice network.habitat.note/a delete",
		"deleted " + string(space),
	}, commitKeys(t, listAll(t, s, spaces.EventFilter{})))
}
