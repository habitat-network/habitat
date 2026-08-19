package simplespace

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	notify_testutil "github.com/habitat-network/habitat/internal/notify/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	orgID     = syntax.DID("did:plc:org")
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

// testManager wires a Manager to the spaces.Store it delegates to for
// repo/record deletion, sharing the same DB and FGA store.
type testManager struct {
	*Manager
	spaces   spaces.Store
	notifier *notify_testutil.TestNotifier
}

func newTestManager(t *testing.T) *testManager {
	t.Helper()

	db := db_testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	spacesStore := spaces_testutil.NewTestStore(t, spaces_testutil.Config{DB: db, FgaStore: fga})

	notifier := &notify_testutil.TestNotifier{}
	return &testManager{
		Manager: &Manager{
			db:       db,
			spaces:   spacesStore.Store,
			fga:      fga,
			notifier: notifier,
			clock:    syntax.NewTIDClock(0),
		},
		spaces:   spacesStore.Store,
		notifier: notifier,
	}
}

func TestCreateSpace(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "my-group")
	require.NoError(t, err)
	require.Equal(t, "at://did:plc:org/space/network.habitat.group/my-group", uri.String())
}

func TestCreateSpace_AutoSkey(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "")
	require.NoError(t, err)
	require.Contains(t, uri, "at://did:plc:org/space/network.habitat.group/")
}

func TestCreateSpace_Duplicate(t *testing.T) {
	m := newTestManager(t)

	_, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "dup")
	require.NoError(t, err)

	_, err = m.CreateSpace(t.Context(), orgID, owner, groupType, "dup")
	require.ErrorIs(t, err, ErrSpaceAlreadyExists)
}

func TestCreateSpace_OwnerIsMember(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := m.IsMember(t.Context(), orgID, uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_Owner(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := m.IsMember(t.Context(), orgID, uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_NonOwner(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := m.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_NonExistentSpace(t *testing.T) {
	m := newTestManager(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	isMember, err := m.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_FGAMember(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := m.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestAddMember_DuplicateIsIdempotent(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	err = m.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)
}

func TestAddMember_OwnerIsAlwaysMember(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.AddMember(t.Context(), uri, owner)
	require.NoError(t, err)
}

func TestAddMember_SpaceNotFound(t *testing.T) {
	m := newTestManager(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := m.AddMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestRemoveMember(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	err = m.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := m.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestRemoveMember_NotAMember(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)
}

func TestRemoveMember_CanNotRemoveOrg(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.RemoveMember(t.Context(), uri, orgID)
	require.ErrorIs(t, err, ErrCannotRemoveOrg)
}

func TestRemoveMember_CanRemoveOwner(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = m.RemoveMember(t.Context(), uri, owner)
	require.NoError(t, err)
}

func TestRemoveMember_NonExistentSpace(t *testing.T) {
	m := newTestManager(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := m.RemoveMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestDeleteSpace(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "to-delete")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = m.spaces.PutRecord(t.Context(), uri, owner, coll, "r1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = m.spaces.PutRecord(t.Context(), uri, owner, coll, "r2", map[string]any{"x": 2})
	require.NoError(t, err)
	require.NoError(t, m.AddMember(t.Context(), uri, alice))

	err = m.DeleteSpace(t.Context(), uri)
	require.NoError(t, err)

	// repos should be gone
	repos, err := m.spaces.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Empty(t, repos)

	// records should be gone
	records, err := m.spaces.ListRecords(t.Context(), uri, owner, nil)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestDeleteSpace_NonExistent(t *testing.T) {
	m := newTestManager(t)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := m.DeleteSpace(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestDeleteSpaceTriggersNotify(t *testing.T) {
	m := newTestManager(t)

	uri, err := m.CreateSpace(t.Context(), orgID, owner, groupType, "doomed")
	require.NoError(t, err)

	require.NoError(t, m.DeleteSpace(t.Context(), uri))
	require.Equal(t, []habitat_syntax.SpaceURI{uri}, m.notifier.Deleted)
}
