package spaces_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/keyval/keyvaltest"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestEventsPostgresTailMissesNothing checks the log's ordering guarantee
// where it matters, on Postgres with concurrent writers: a reader tailing
// the log from its cursor while writes land must end up having seen every
// record's final write. Seqs that became visible out of order would let the
// cursor pass a write that committed late, which this would catch.
func TestEventsPostgresTailMissesNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Postgres")
	}
	d := keyvaltest.NewDB(t)
	require.NoError(t, db.Migrate(t.Context(), d, nil, spaces.Models()))
	// The default test notifier isn't safe for concurrent writers.
	s := spaces_testutil.NewTestStore(t,
		spaces_testutil.WithDB(d), spaces_testutil.WithNotifier(spaces.Notifiers{}))
	ctx := t.Context()

	const writers, writesEach = 4, 25
	spaceURIs := make([]habitat_syntax.SpaceURI, writers)
	for i := range spaceURIs {
		var err error
		spaceURIs[i], err = s.CreateSpace(
			ctx,
			orgID,
			groupType,
			habitat_syntax.SpaceKey(fmt.Sprint(i)),
		)
		require.NoError(t, err)
	}

	var wg sync.WaitGroup
	done := make(chan struct{})
	for w := range writers {
		wg.Go(func() {
			repo := syntax.DID(fmt.Sprintf("did:plc:writer%d", w))
			for i := range writesEach {
				// Rewrite a few records over and over, so the log compacts.
				rkey := syntax.RecordKey(fmt.Sprint(i % 5))
				putNote(t, s, spaceURIs[w], repo, noteCollection, rkey, fmt.Sprint(i))
			}
		})
	}
	// Several sequencers at once, as with several pear instances.
	for range 2 {
		wg.Go(func() {
			for {
				select {
				case <-done:
					return
				default:
				}
				_, err := s.SequenceEvents(ctx)
				require.NoError(t, err)
			}
		})
	}

	// The tail: what it has seen of each record, by latest seq.
	seen := map[string]string{}
	var cursor spaces.EventSeq
	read := func() {
		for {
			events, err := s.ListEvents(ctx, spaces.EventFilter{}, cursor, 10)
			require.NoError(t, err)
			for _, e := range events {
				require.GreaterOrEqual(t, e.EventSeq(), cursor)
				commit, ok := e.(spaces.CommitEvent)
				require.True(t, ok)
				key := string(commit.Space) + " " + string(commit.Repo) + " " + string(commit.Rkey)
				text, ok := commit.Value["text"].(string)
				require.True(t, ok)
				seen[key] = text
				cursor = e.EventSeq() + 1
			}
			if len(events) < 10 {
				return
			}
		}
	}
	writing := make(chan struct{})
	go func() {
		defer close(writing)
		wg.Wait()
	}()
	// Keep tailing while writers and sequencers run; the sequencers only stop
	// once told to, after the writers are done.
	for range writers * writesEach {
		read()
	}
	close(done)
	<-writing
	_, err := s.SequenceEvents(ctx)
	require.NoError(t, err)
	read()

	for w := range writers {
		for rkey := range 5 {
			key := fmt.Sprintf("%s did:plc:writer%d %d", spaceURIs[w], w, rkey)
			// The last write to rkey r is the largest i < writesEach with i%5 == r.
			want := fmt.Sprint(writesEach - 5 + rkey)
			require.Equal(t, want, seen[key], key)
		}
	}
}

func TestEventsPostgresSpaceDeleted(t *testing.T) {
	if testing.Short() {
		t.Skip("starts Postgres")
	}
	d := keyvaltest.NewDB(t)
	require.NoError(t, db.Migrate(t.Context(), d, nil, spaces.Models()))
	s := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(d))
	ctx := t.Context()

	space, err := s.CreateSpace(ctx, orgID, groupType, "doomed")
	require.NoError(t, err)
	putNote(t, s, space, alice, noteCollection, "a", "one")
	require.NoError(t, s.DeleteSpace(ctx, space))
	require.Equal(t, []string{
		"did:plc:alice network.habitat.note/a delete",
		"deleted " + string(space),
	}, commitKeys(t, listAll(t, s, spaces.EventFilter{
		Spaces: []habitat_syntax.SpaceURI{space},
	})))
}
