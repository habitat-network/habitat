package simplespace

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"

	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	orgID     = syntax.DID("did:plc:org")
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

func newTestStore(t *testing.T) *Store {
	t.Helper()

	db := db_testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	spacesStore := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))
	permsStore := perms.NewStore(db, spacesStore, fga)
	return NewStore(db, spacesStore, permsStore)
}

func TestCreateSpace(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "my-group")
	require.NoError(t, err)
	require.Equal(t, "at://did:plc:org/space/network.habitat.group/my-group", uri.String())
}

func TestCreateSpace_AutoSkey(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "")
	require.NoError(t, err)
	require.Contains(t, uri, "at://did:plc:org/space/network.habitat.group/")
}

func TestCreateSpace_Duplicate(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "dup")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), orgID, owner, groupType, "dup")
	require.ErrorIs(t, err, ErrSpaceAlreadyExists)
}

func TestCreateSpace_OwnerIsMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgID, uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_Owner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgID, uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_NonOwner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_NonExistentSpace(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	isMember, err := s.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_FGAMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestAddMember_DuplicateIsIdempotent(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)
}

func TestAddMember_OwnerIsAlwaysMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, owner)
	require.NoError(t, err)
}

func TestAddMember_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.AddMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestRemoveMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestRemoveMember_NotAMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)
}

func TestRemoveMember_CanNotRemoveOrg(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, orgID)
	require.ErrorIs(t, err, ErrCannotRemoveOrg)
}

func TestRemoveMember_CanRemoveOwner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, owner)
	require.NoError(t, err)
}

func TestRemoveMember_NonExistentSpace(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.RemoveMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestDeleteSpace(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "to-delete")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, _, err = s.spaces.PutRecord(t.Context(), uri, owner, coll, "r1", map[string]any{"x": 1})
	require.NoError(t, err)
	_, _, err = s.spaces.PutRecord(t.Context(), uri, owner, coll, "r2", map[string]any{"x": 2})
	require.NoError(t, err)
	require.NoError(t, s.AddMember(t.Context(), uri, alice))

	err = s.DeleteSpace(t.Context(), uri)
	require.NoError(t, err)

	// the space itself, and the repos in it, are gone
	_, err = s.spaces.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)

	// records should be gone
	records, err := s.spaces.ListRecords(t.Context(), uri, owner, nil)
	require.NoError(t, err)
	require.Len(t, records, 0)
}

func TestDeleteSpace_NonExistent(t *testing.T) {
	s := newTestStore(t)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.DeleteSpace(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

// TestDeleteSpace_RemovesFGATuples pins the behavior that distinguishes
// Manager.DeleteSpace from the bare spaces.Store.DeleteSpace it wraps:
// cleaning up every FGA tuple the space accumulated. A leftover tuple would
// silently keep granting access to a space that no longer exists, and could
// collide with a future space created at the same URI.
func TestDeleteSpace_RemovesFGATuples(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "to-delete")
	require.NoError(t, err)
	require.NoError(t, s.AddMember(t.Context(), uri, alice))

	require.NoError(t, s.DeleteSpace(t.Context(), uri))

	recs, err := s.spaces.ListRecords(t.Context(), uri, orgID, nil)
	require.NoError(t, err)
	require.Len(t, recs, 0)
}

func TestDeleteSpaceTriggersNotify(t *testing.T) {
	db := db_testutil.NewDB(t)
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	spacesStore := spaces_testutil.NewTestStore(t, spaces_testutil.WithDB(db), spaces_testutil.WithFGA(fga))
	permsStore := perms.NewStore(db, spacesStore, fga)
	s := NewStore(db, spacesStore, permsStore)

	uri, err := s.CreateSpace(t.Context(), orgID, owner, groupType, "doomed")
	require.NoError(t, err)

	require.NoError(t, s.DeleteSpace(t.Context(), uri))

	ok, err := s.spaces.CheckSpaceExists(t.Context(), uri)
	require.NoError(t, err)
	require.False(t, ok)
}
