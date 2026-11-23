package spacestream

import (
	"encoding/json"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/spacescope"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func snapshotRkeys(t *testing.T, page SnapshotPage) []string {
	t.Helper()
	var out []string
	for _, raw := range page.Events {
		var e struct {
			Type string `json:"$type"`
			Rkey string `json:"rkey"`
			Seq  int64  `json:"seq"`
		}
		require.NoError(t, json.Unmarshal(raw, &e))
		require.Contains(t, e.Type, "network.habitat.space.subscribe#")
		out = append(out, e.Rkey)
	}
	return out
}

func TestSnapshotPagesToAPinnedTip(t *testing.T) {
	log := &fakeLog{}
	roles := &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{spaceA: true}}
	for _, k := range []string{"1", "2", "3", "4", "5"} {
		log.add(spaceA, syntax.RecordKey(k))
	}
	server := NewServer(log, roles, NewBroadcaster(), WithPageSize(2))
	sub := Subscription{Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}}}

	page, err := server.Snapshot(t.Context(), sub, SnapshotQuery{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, snapshotRkeys(t, page))
	require.Equal(t, spaces.EventSeq(2), page.PlannedThroughSeq)
	require.Equal(t, spaces.EventSeq(5), page.TipSeq)

	// A write after the first page is beyond the pinned tip.
	log.add(spaceA, "6")
	tip := page.TipSeq
	var got []string
	for page.PlannedThroughSeq < page.TipSeq {
		page, err = server.Snapshot(t.Context(), sub, SnapshotQuery{
			After: page.PlannedThroughSeq, Before: &tip, Limit: 2,
		})
		require.NoError(t, err)
		got = append(got, snapshotRkeys(t, page)...)
	}
	require.Equal(t, []string{"3", "4", "5"}, got)
}

func TestSnapshotAuthorizesLikeAStream(t *testing.T) {
	log := &fakeLog{}
	roles := &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{spaceA: true}}
	log.add(spaceB, "1")
	log.add(spaceB, "2")
	log.add(spaceA, "3")
	log.deleteSpace(spaceA)
	server := NewServer(log, roles, NewBroadcaster(), WithPageSize(1))

	// Pages of unreadable events still advance plannedThroughSeq.
	page, err := server.Snapshot(t.Context(),
		Subscription{Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}}},
		SnapshotQuery{Limit: 1})
	require.NoError(t, err)
	require.Equal(t, []string{"3"}, snapshotRkeys(t, page))
	require.Equal(t, spaces.EventSeq(3), page.PlannedThroughSeq)

	// A space credential gets its space alone, and a conflicting filter is refused.
	cred := Subscription{Reader: SpaceCredentialReader{Space: spaceB}}
	page, err = server.Snapshot(t.Context(), cred, SnapshotQuery{Limit: 10})
	require.NoError(t, err)
	require.Equal(t, []string{"1", "2"}, snapshotRkeys(t, page))
	_, err = server.Snapshot(t.Context(), cred, SnapshotQuery{
		Limit: 10, Filter: spaces.EventFilter{Spaces: []habitat_syntax.SpaceURI{spaceA}},
	})
	require.ErrorIs(t, err, ErrCredentialMismatch)
}

func TestSnapshotEmptyLogAndAfterTip(t *testing.T) {
	log := &fakeLog{}
	server := NewServer(log, &fakeRoles{}, NewBroadcaster())
	sub := Subscription{Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}}}
	page, err := server.Snapshot(t.Context(), sub, SnapshotQuery{Limit: 10})
	require.NoError(t, err)
	require.Empty(t, page.Events)
	require.Zero(t, page.TipSeq)

	log.add(spaceA, "1")
	page, err = server.Snapshot(t.Context(), sub, SnapshotQuery{After: 9, Limit: 10})
	require.NoError(t, err)
	require.Empty(t, page.Events)
	require.Equal(t, spaces.EventSeq(1), page.PlannedThroughSeq)
}

func TestParseSnapshotQuery(t *testing.T) {
	q, err := ParseSnapshotQuery(url.Values{
		"afterSeq": {"5"}, "beforeSeq": {"9"}, "limit": {"50"},
		"collections": {"network.habitat.*"},
	})
	require.NoError(t, err)
	require.Equal(t, spaces.EventSeq(5), q.After)
	require.Equal(t, spaces.EventSeq(9), *q.Before)
	require.Equal(t, 50, q.Limit)
	require.Equal(t, []string{"network.habitat."}, q.Filter.CollectionPrefixes)

	q, err = ParseSnapshotQuery(url.Values{})
	require.NoError(t, err)
	require.Nil(t, q.Before)
	require.Equal(t, DefaultSnapshotLimit, q.Limit)

	for _, bad := range []url.Values{
		{"afterSeq": {"-1"}}, {"beforeSeq": {"x"}}, {"limit": {"0"}}, {"limit": {"1001"}},
	} {
		_, err = ParseSnapshotQuery(bad)
		require.ErrorIs(t, err, ErrInvalidQuery)
	}
}
