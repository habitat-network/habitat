package spaces

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	db := testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	s, err := NewStore(db, fga, eventStore, nil)
	require.NoError(t, err)
	return s
}

// recordingNotifier captures Notifier calls for assertions.
type recordingNotifier struct {
	calls   []notifyCall
	deleted []habitat_syntax.SpaceURI
}

type notifyCall struct {
	space habitat_syntax.SpaceURI
	repo  syntax.DID
	rev   string
}

func (n *recordingNotifier) NotifyWrite(
	_ context.Context,
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	rev string,
) {
	n.calls = append(n.calls, notifyCall{space: space, repo: repo, rev: rev})
}

func (n *recordingNotifier) NotifySpaceDeleted(
	_ context.Context,
	space habitat_syntax.SpaceURI,
) {
	n.deleted = append(n.deleted, space)
}

var (
	orgId     = syntax.DID("did:plc:org")
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

func TestCreateSpace(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "my-group")
	require.NoError(t, err)
	require.Equal(t, "ats://did:plc:org/network.habitat.group/my-group", uri.String())
}

func TestCreateSpace_AutoSkey(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "")
	require.NoError(t, err)
	require.Contains(t, uri, "ats://did:plc:org/network.habitat.group/")
}

func TestCreateSpace_Duplicate(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "dup")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), orgId, owner, groupType, "dup")
	require.ErrorIs(t, err, ErrSpaceAlreadyExists)
}

func TestCreateSpace_OwnerIsMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgId, uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestListSpaces(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "space1")
	require.NoError(t, err)

	spaceUri2, err := s.CreateSpace(t.Context(), orgId, alice, groupType, "space2")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), spaceUri2, owner, SpaceAccessRead)
	require.NoError(t, err)

	// Owner should see both
	spaces, err := s.ListSpaces(t.Context(), orgId, owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 2)

	// memberCount should be populated (at least 1 — the owner)
	for _, sp := range spaces {
		require.GreaterOrEqual(t, sp.MemberCount, 1)
	}
	// Alice should see 1
	spaces, err = s.ListSpaces(t.Context(), orgId, alice, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
}

func TestListSpaces_FilterByType(t *testing.T) {
	s := newTestStore(t)

	personal := syntax.NSID("network.habitat.personal")

	_, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "group1")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), orgId, owner, personal, "personal1")
	require.NoError(t, err)

	spaces, err := s.ListSpaces(t.Context(), orgId, owner, nil, &groupType)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, groupType, spaces[0].Type)
}

func TestListSpaces_NilOwnerFilterSpansAllOrgs(t *testing.T) {
	s := newTestStore(t)

	orgA := syntax.DID("did:plc:org-a")
	orgB := syntax.DID("did:plc:org-b")
	member := syntax.DID("did:plc:cross-org-member")

	spaceA, err := s.CreateSpace(t.Context(), orgA, member, groupType, "space-a")
	require.NoError(t, err)
	spaceB, err := s.CreateSpace(t.Context(), orgB, member, groupType, "space-b")
	require.NoError(t, err)

	// With no owner filter, the member sees spaces across every org they
	// belong to, not just one.
	spaces, err := s.ListSpaces(t.Context(), orgA, member, nil, nil)
	require.NoError(t, err)
	uris := make([]habitat_syntax.SpaceURI, len(spaces))
	for i, sp := range spaces {
		uris[i] = sp.URI
	}
	require.ElementsMatch(t, []habitat_syntax.SpaceURI{spaceA, spaceB}, uris)

	// Filtering by a specific org owner restricts the results to that org.
	spaces, err = s.ListSpaces(t.Context(), orgA, member, &orgA, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, spaceA, spaces[0].URI)
}

func TestListRepos_Empty(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	repos, err := s.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Empty(t, repos)
}

func TestListRepos_WithRecords(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, alice, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	repos, err := s.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, repos, 2)

	dids := make([]syntax.DID, len(repos))
	for i, r := range repos {
		dids[i] = r.DID
	}
	require.ElementsMatch(t, []syntax.DID{owner, alice}, dids)
	require.NotEmpty(t, repos[0].Rev)
}

func TestListRepos_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	_, err := s.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestIsMember_Owner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgId, uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_NonOwner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgId, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_NonExistentSpace(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	isMember, err := s.IsMember(t.Context(), orgId, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_FGAMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgId, uri, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestAddMember_DuplicateIsIdempotent(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.NoError(t, err)
}

func TestAddMember_OwnerIsAlwaysMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, owner, SpaceAccessRead)
	require.NoError(t, err)
}

func TestAddMember_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestRemoveMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgId, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestRemoveMember_NotAMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)
}

func TestRemoveMember_CanNotRemoveOrg(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, orgId)
	require.ErrorIs(t, err, ErrCannotRemoveOrg)
}

func TestRemoveMember_CanRemoveOwner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, owner)
	require.NoError(t, err)
}

func TestRemoveMember_NonExistentSpace(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.RemoveMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestPutAndGetRecord(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	val := map[string]any{"text": "hello world"}

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "my-rkey", val)
	require.NoError(t, err)

	rec, err := s.GetRecord(t.Context(), uri, owner, coll, "my-rkey")
	require.NoError(t, err)
	require.Equal(t, val, rec.Value)
	require.Equal(t, syntax.RecordKey("my-rkey"), rec.Rkey)
}

func TestPutRecord_UpdateExisting(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"v": 1})
	require.NoError(t, err)

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"v": 2})
	require.NoError(t, err)

	rec, err := s.GetRecord(t.Context(), uri, owner, coll, "rkey")
	require.NoError(t, err)
	require.Equal(t, int64(2), rec.Value["v"])
}

func TestGetRecord_NotFound(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, err = s.GetRecord(t.Context(), uri, owner, coll, "nonexistent")
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestListRecords(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	collA := syntax.NSID("network.habitat.alpha")
	collB := syntax.NSID("network.habitat.beta")
	_, _, err = s.PutRecord(t.Context(), uri, owner, collA, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, collA, "k2", map[string]any{"x": 2})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, collB, "k1", map[string]any{"x": 3})
	require.NoError(t, err)

	// All records
	records, err := s.ListRecords(t.Context(), uri, owner, nil)
	require.NoError(t, err)
	require.Len(t, records, 3)

	// Filter by collection
	records, err = s.ListRecords(t.Context(), uri, owner, &collA)
	require.NoError(t, err)
	require.Len(t, records, 2)
	for _, r := range records {
		require.Equal(t, collA, r.Collection)
	}
}

func TestDeleteRecord(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"x": 1})
	require.NoError(t, err)

	err = s.DeleteRecord(t.Context(), uri, coll, "rkey")
	require.NoError(t, err)

	_, err = s.GetRecord(t.Context(), uri, owner, coll, "rkey")
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestDeleteRecord_Nonexistent(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	// Deleting a nonexistent record should not error
	err = s.DeleteRecord(t.Context(), uri, syntax.NSID("network.habitat.note"), "nonexistent")
	require.NoError(t, err)
}

func TestSpaceURI(t *testing.T) {
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "my-key")
	require.Equal(t, "ats://did:plc:owner/network.habitat.group/my-key", uri.String())
	require.Equal(t, owner, uri.SpaceOwner())
	require.Equal(t, groupType, uri.SpaceType())
	require.Equal(t, habitat_syntax.SpaceKey("my-key"), uri.Skey())

	parsed, err := habitat_syntax.ParseSpaceURI("ats://did:plc:owner/network.habitat.group/my-key")
	require.NoError(t, err)
	require.Equal(t, uri, parsed)
}

func TestParseSpaceURI_Invalid(t *testing.T) {
	tests := []struct {
		input string
		name  string
	}{
		{"", "empty"},
		{"ats://notadid/network.habitat.group/key", "invalid did"},
		{"notaspace", "no scheme"},
		{"ats://did:plc:abc", "missing type and key"},
		{"ats://did:plc:abc/notansid/key", "invalid type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := habitat_syntax.ParseSpaceURI(tt.input)
			require.Error(t, err)
		})
	}
}

func TestDeleteSpace(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "to-delete")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "r1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "r2", map[string]any{"x": 2})
	require.NoError(t, err)
	require.NoError(t, s.AddMember(t.Context(), uri, alice, SpaceAccessRead))

	err = s.DeleteSpace(t.Context(), uri)
	require.NoError(t, err)

	// space should be gone
	_, err = s.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, ErrSpaceNotFound)

	// records should be gone
	records, err := s.ListRecords(t.Context(), uri, owner, nil)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestDeleteSpace_NonExistent(t *testing.T) {
	s := newTestStore(t)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.DeleteSpace(t.Context(), uri)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestListRepoOps(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, alice, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k3", map[string]any{"x": 3})
	require.NoError(t, err)

	records, err := s.ListRepoOps(t.Context(), uri, owner, "", 1)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, syntax.RecordKey("k1"), records[0].Rkey)

	records, err = s.ListRepoOps(t.Context(), uri, owner, records[0].Rev, 100)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, syntax.RecordKey("k3"), records[0].Rkey)

	records, err = s.ListRepoOps(t.Context(), uri, alice, "", 100)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, syntax.RecordKey("k2"), records[0].Rkey)
}

func TestListRepoOps_Empty(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	records, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestListRepoOps_RevIncludesValue(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"text": "hello"})
	require.NoError(t, err)

	records, err := s.ListRepoOps(t.Context(), uri, owner, "", 100)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "hello", records[0].Value["text"])
	require.NotEmpty(t, records[0].Rev)
}

func TestPutRecordTriggersNotify(t *testing.T) {
	db := testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	notifier := &recordingNotifier{}
	s, err := NewStore(db, fga, eventStore, notifier)
	require.NoError(t, err)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "notify-space")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	require.Len(t, notifier.calls, 1)
	require.Equal(t, uri, notifier.calls[0].space)
	require.Equal(t, owner, notifier.calls[0].repo)
	require.NotEmpty(t, notifier.calls[0].rev)
}

func TestDeleteSpaceTriggersNotify(t *testing.T) {
	db := testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	notifier := &recordingNotifier{}
	s, err := NewStore(db, fga, eventStore, notifier)
	require.NoError(t, err)

	uri, err := s.CreateSpace(t.Context(), orgId, owner, groupType, "doomed")
	require.NoError(t, err)

	require.NoError(t, s.DeleteSpace(t.Context(), uri))
	require.Equal(t, []habitat_syntax.SpaceURI{uri}, notifier.deleted)
}

func TestDeleteSpaceNotFoundDoesNotNotify(t *testing.T) {
	db := testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	eventStore, err := events.NewStore(db)
	require.NoError(t, err)
	notifier := &recordingNotifier{}
	s, err := NewStore(db, fga, eventStore, notifier)
	require.NoError(t, err)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "missing")
	require.ErrorIs(t, s.DeleteSpace(t.Context(), uri), ErrSpaceNotFound)
	require.Empty(t, notifier.deleted)
}
