package notify

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	habitatdb "github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	space = habitat_syntax.SpaceURI("ats://did:plc:org/network.habitat.group/s1")
	repo  = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	db := testutil.NewDB(t)
	s := NewStore(db)
	require.NoError(t, habitatdb.AutoMigrate(db, s))
	return s
}

func TestStoreRegisterAndListForRepo(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)

	// whole-space registration
	require.NoError(t, s.Register(t.Context(), space, "", "https://sync.example/all", future))
	// repo-specific registration matching the write
	require.NoError(t, s.Register(t.Context(), space, repo, "https://sync.example/alice", future))
	// repo-specific registration for a different repo — must not match
	require.NoError(t, s.Register(t.Context(), space, bob, "https://sync.example/bob", future))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)

	endpoints := make([]string, len(regs))
	for i, r := range regs {
		endpoints[i] = r.Endpoint
	}
	require.ElementsMatch(
		t,
		[]string{"https://sync.example/all", "https://sync.example/alice"},
		endpoints,
	)
}

func TestStoreRegisterRefreshesExpiry(t *testing.T) {
	s := newTestStore(t)
	first := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	second := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)

	require.NoError(t, s.Register(t.Context(), space, repo, "https://sync.example/alice", first))
	require.NoError(t, s.Register(t.Context(), space, repo, "https://sync.example/alice", second))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.WithinDuration(t, second, regs[0].ExpiresAt, time.Second)
}

func TestStoreListForSpace(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)
	other := habitat_syntax.SpaceURI("ats://did:plc:org/network.habitat.group/other")

	require.NoError(t, s.Register(t.Context(), space, "", "https://sync.example/all", future))
	require.NoError(t, s.Register(t.Context(), space, repo, "https://sync.example/alice", future))
	require.NoError(t, s.Register(t.Context(), space, bob, "https://sync.example/bob", future))
	// A registration for a different space must not be returned.
	require.NoError(t, s.Register(t.Context(), other, "", "https://sync.example/other", future))
	// An expired registration for the space must be excluded.
	past := time.Now().Add(-time.Hour)
	require.NoError(t, s.Register(t.Context(), space, "", "https://sync.example/expired", past))

	regs, err := s.ListForSpace(t.Context(), space)
	require.NoError(t, err)

	endpoints := make([]string, len(regs))
	for i, r := range regs {
		endpoints[i] = r.Endpoint
	}
	require.ElementsMatch(t, []string{
		"https://sync.example/all",
		"https://sync.example/alice",
		"https://sync.example/bob",
	}, endpoints)
}

func TestStoreListForRepoExcludesExpired(t *testing.T) {
	s := newTestStore(t)
	past := time.Now().Add(-time.Hour)

	require.NoError(t, s.Register(t.Context(), space, "", "https://sync.example/all", past))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Empty(t, regs)
}
