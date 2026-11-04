package notify

import (
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	space = habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.group/s1")
	repo  = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	s, err := NewStore(testutil.NewDB(t, Models()))
	require.NoError(t, err)
	return s
}

// registerLegacy registers through the deprecated endpoint field, where the
// endpoint is both the delivery address and the audience. Keeping it to one
// helper stops the repeated pair from crowding out the intent of each case.
func registerLegacy(
	t *testing.T, s Store, space habitat_syntax.SpaceURI, repo syntax.DID,
	endpoint string, expiresAt time.Time,
) {
	t.Helper()
	require.NoError(
		t, s.Register(t.Context(), NamespaceHabitat, space, repo, endpoint, endpoint, expiresAt),
	)
}

func TestStoreRegisterAndListForRepo(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)

	// whole-space registration
	registerLegacy(t, s, space, "", "https://sync.example/all", future)
	// repo-specific registration matching the write
	registerLegacy(t, s, space, repo, "https://sync.example/alice", future)
	// repo-specific registration for a different repo — must not match
	registerLegacy(t, s, space, bob, "https://sync.example/bob", future)

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

	registerLegacy(t, s, space, repo, "https://sync.example/alice", first)
	registerLegacy(t, s, space, repo, "https://sync.example/alice", second)

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.WithinDuration(t, second, regs[0].ExpiresAt, time.Second)
}

func TestStoreListForSpace(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)
	other := habitat_syntax.SpaceURI("at://did:plc:org/space/network.habitat.group/other")

	registerLegacy(t, s, space, "", "https://sync.example/all", future)
	registerLegacy(t, s, space, repo, "https://sync.example/alice", future)
	registerLegacy(t, s, space, bob, "https://sync.example/bob", future)
	// A registration for a different space must not be returned.
	registerLegacy(t, s, other, "", "https://sync.example/other", future)
	// An expired registration for the space must be excluded.
	past := time.Now().Add(-time.Hour)
	registerLegacy(t, s, space, "", "https://sync.example/expired", past)

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

	registerLegacy(t, s, space, "", "https://sync.example/all", past)

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Empty(t, regs)
}

const syncerService = "did:web:sync.example.com#habitat_space_syncer"

// TestStoreRegisterRecordsAudience covers the service-identifier path: the
// registration keeps the audience it is addressed by alongside the endpoint it
// resolved to.
func TestStoreRegisterRecordsAudience(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)

	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", syncerService, "https://sync.example", future,
	))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, syncerService, regs[0].Audience)
	require.Equal(t, "https://sync.example", regs[0].Endpoint)
}

// TestStoreReRegisteringSameAudienceMovesEndpoint is what keying on audience
// buys: a service that now resolves somewhere else updates the registration it
// already owns rather than accumulating a second row and being notified twice.
func TestStoreReRegisteringSameAudienceMovesEndpoint(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)

	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", syncerService, "https://old.example", future,
	))
	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", syncerService, "https://moved.example", future,
	))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1, "re-registering the same service should update in place")
	require.Equal(t, syncerService, regs[0].Audience)
	require.Equal(t, "https://moved.example", regs[0].Endpoint)
}

// TestStoreLegacyEndpointIsItsOwnAudience pins a registration made through the
// deprecated endpoint field: the URL is both the delivery address and the
// audience, which is what the audience migration writes for existing rows.
func TestStoreLegacyEndpointIsItsOwnAudience(t *testing.T) {
	s := newTestStore(t)

	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", "https://sync.example", "https://sync.example",
		time.Now().Add(time.Hour),
	))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 1)
	require.Equal(t, "https://sync.example", regs[0].Audience)
	require.Equal(t, "https://sync.example", regs[0].Endpoint)
}

// TestStoreKeepsDistinctLegacyEndpoints verifies two subscribers that each
// registered only an endpoint keep separate registrations, since each endpoint
// is its own audience.
func TestStoreKeepsDistinctLegacyEndpoints(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)

	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", "https://a.example", "https://a.example", future,
	))
	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", "https://b.example", "https://b.example", future,
	))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 2)
}

// TestStoreKeysDistinctAudiencesOnSameEndpoint covers the flip side of keying on
// audience rather than endpoint: two services that resolve to the same address
// stay separate registrations, since the audience is what identifies a
// subscriber. The whole-space registration is the one that matches a write to
// any repo, so it has to be the one the pair is checked through.
func TestStoreKeysDistinctAudiencesOnSameEndpoint(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)

	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", syncerService, "https://sync.example", future,
	))
	require.NoError(t, s.Register(
		t.Context(), NamespaceHabitat, space, "", "did:web:other.example#habitat_space_syncer",
		"https://sync.example", future,
	))

	regs, err := s.ListForRepo(t.Context(), space, repo)
	require.NoError(t, err)
	require.Len(t, regs, 2, "distinct subscribers sharing an address stay distinct")
	// Both audiences survive, whichever order the query returned them in.
	audiences := []string{regs[0].Audience, regs[1].Audience}
	require.ElementsMatch(
		t,
		[]string{syncerService, "did:web:other.example#habitat_space_syncer"},
		audiences,
	)
	// Same address, reached by two independent registrations.
	require.Equal(t, regs[0].Endpoint, regs[1].Endpoint)
}

func TestStoreSpaceDeletedDeliveries(t *testing.T) {
	s := newTestStore(t)
	future := time.Now().Add(time.Hour)
	later := future.Add(time.Hour)
	regs := []Registration{
		// One audience registered twice is delivered to once, under its
		// longest-lived registration.
		{
			Space: space, Audience: "did:web:a#svc", Endpoint: "https://a",
			Namespace: NamespaceAtproto, ExpiresAt: future,
		},
		{
			Space: space, Repo: repo, Audience: "did:web:a#svc", Endpoint: "https://a",
			Namespace: NamespaceAtproto, ExpiresAt: later,
		},
		{
			Space: space, Audience: "https://b", Endpoint: "https://b",
			Namespace: NamespaceHabitat, ExpiresAt: future,
		},
	}
	pending, err := s.QueueSpaceDeleted(t.Context(), space, regs, 100)
	require.NoError(t, err)
	require.Len(t, pending, 2)

	due, err := s.DueSpaceDeleted(t.Context(), 99, 10)
	require.NoError(t, err)
	require.Empty(t, due)
	due, err = s.DueSpaceDeleted(t.Context(), 100, 10)
	require.NoError(t, err)
	require.Len(t, due, 2)
	byAudience := map[string]PendingDeletion{}
	for _, p := range due {
		byAudience[p.Audience] = p
	}
	require.Equal(t, NamespaceAtproto, byAudience["did:web:a#svc"].Namespace)
	require.WithinDuration(t, later, byAudience["did:web:a#svc"].ExpiresAt, time.Second)
	require.Equal(t, "https://b", byAudience["https://b"].Endpoint)

	// Only the first of two workers holding the same read wins the claim.
	a := byAudience["did:web:a#svc"]
	ok, err := s.RescheduleSpaceDeleted(t.Context(), a, a.Attempts, 200)
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = s.RescheduleSpaceDeleted(t.Context(), a, a.Attempts, 300)
	require.NoError(t, err)
	require.False(t, ok)

	// A stale clear leaves the claimed row alone; a current one drops it.
	require.NoError(t, s.ClearSpaceDeleted(t.Context(), a))
	due, err = s.DueSpaceDeleted(t.Context(), 200, 10)
	require.NoError(t, err)
	require.Len(t, due, 2)
	a.RetryAt = 200
	require.NoError(t, s.ClearSpaceDeleted(t.Context(), a))
	due, err = s.DueSpaceDeleted(t.Context(), 200, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)

	// Deleting the space again re-queues from the first attempt.
	b := due[0]
	ok, err = s.RescheduleSpaceDeleted(t.Context(), b, 3, 150)
	require.NoError(t, err)
	require.True(t, ok)
	_, err = s.QueueSpaceDeleted(t.Context(), space, regs[2:], 120)
	require.NoError(t, err)
	due, err = s.DueSpaceDeleted(t.Context(), 200, 10)
	require.NoError(t, err)
	require.Len(t, due, 1)
	require.Equal(t, 0, due[0].Attempts)
	require.Equal(t, int64(120), due[0].RetryAt)
}
