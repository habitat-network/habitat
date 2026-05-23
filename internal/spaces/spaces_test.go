package spaces

import (
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	require.NoError(t, err)
	fga, err := fgastore.NewSQLite(t.Context(), filepath.Join(t.TempDir(), "fga.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	s, err := NewStore(db, fga)
	require.NoError(t, err)
	return s
}

var (
	owner     = syntax.DID("did:plc:owner")
	alice     = syntax.DID("did:plc:alice")
	groupType = syntax.NSID("network.habitat.group")
)

func TestCreateSpace(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "my-group")
	require.NoError(t, err)
	require.Equal(t, "ats://did:plc:owner/network.habitat.group/my-group", uri.String())
}

func TestCreateSpace_AutoSkey(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "")
	require.NoError(t, err)
	require.NotEmpty(t, uri.Skey())
	require.NotEqual(t, "", uri.Skey())
}

func TestCreateSpace_Duplicate(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateSpace(t.Context(), owner, groupType, "dup")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), owner, groupType, "dup")
	require.ErrorIs(t, err, ErrSpaceAlreadyExists)
}

func TestCreateSpace_OwnerIsMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestListSpaces(t *testing.T) {
	s := newTestStore(t)

	_, err := s.CreateSpace(t.Context(), owner, groupType, "space1")
	require.NoError(t, err)

	spaceUri2, err := s.CreateSpace(t.Context(), alice, groupType, "space2")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), spaceUri2, owner)
	require.NoError(t, err)

	// Owner should see both
	spaces, err := s.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 2)

	// memberCount should be populated (at least 1 — the owner)
	for _, sp := range spaces {
		require.GreaterOrEqual(t, sp.MemberCount, 1)
	}
	// Alice should see 1
	spaces, err = s.ListSpaces(t.Context(), alice, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
}

func TestListSpaces_FilterByType(t *testing.T) {
	s := newTestStore(t)

	personal := syntax.NSID("network.habitat.personal")

	_, err := s.CreateSpace(t.Context(), owner, groupType, "group1")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), owner, personal, "personal1")
	require.NoError(t, err)

	spaces, err := s.ListSpaces(t.Context(), owner, nil, &groupType)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, groupType, spaces[0].Type)
}

func TestGetMembers(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	members, err := s.GetMembers(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, members, 1)
	require.Equal(t, owner, members[0].Did)
}

func TestGetMembers_WithAddedMembers(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	members, err := s.GetMembers(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, members, 2)

	dids := []syntax.DID{members[0].Did, members[1].Did}
	require.Contains(t, dids, owner)
	require.Contains(t, dids, alice)
}

func TestGetMembers_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	_, err := s.GetMembers(t.Context(), uri)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestIsMember_Owner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), uri, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_NonOwner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_NonExistentSpace(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	isMember, err := s.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestIsMember_FGAMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestAddMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)
}

func TestAddMember_AlreadyMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, ErrUserAlreadyMember)
}

func TestAddMember_OwnerIsAlwaysMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, owner)
	require.ErrorIs(t, err, ErrUserAlreadyMember)
}

func TestAddMember_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.AddMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestRemoveMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, alice)
	require.NoError(t, err)

	isMember, err := s.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestRemoveMember_NotAMember(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, ErrNotAMember)
}

func TestRemoveMember_CannotRemoveOwner(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.RemoveMember(t.Context(), uri, owner)
	require.ErrorIs(t, err, ErrCannotRemoveOwner)
}

func TestRemoveMember_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	err := s.RemoveMember(t.Context(), uri, alice)
	require.ErrorIs(t, err, ErrNotAMember)
}

func TestPutAndGetRecord(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	val := map[string]any{"text": "hello world"}

	err = s.PutRecord(t.Context(), uri, owner, coll, "my-rkey", val)
	require.NoError(t, err)

	rec, err := s.GetRecord(t.Context(), uri, owner, coll, "my-rkey")
	require.NoError(t, err)
	require.Equal(t, val, rec.Value)
	require.Equal(t, syntax.RecordKey("my-rkey"), rec.Rkey)
}

func TestPutRecord_UpdateExisting(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"v": 1})
	require.NoError(t, err)

	err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"v": 2})
	require.NoError(t, err)

	rec, err := s.GetRecord(t.Context(), uri, owner, coll, "rkey")
	require.NoError(t, err)
	require.Equal(t, float64(2), rec.Value["v"])
}

func TestGetRecord_NotFound(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	_, err = s.GetRecord(t.Context(), uri, owner, coll, "nonexistent")
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestListRecords(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	collA := syntax.NSID("network.habitat.alpha")
	collB := syntax.NSID("network.habitat.beta")

	require.NoError(t, s.PutRecord(t.Context(), uri, owner, collA, "k1", map[string]any{"x": 1}))
	require.NoError(t, s.PutRecord(t.Context(), uri, owner, collA, "k2", map[string]any{"x": 2}))
	require.NoError(t, s.PutRecord(t.Context(), uri, owner, collB, "k1", map[string]any{"x": 3}))

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

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	err = s.PutRecord(t.Context(), uri, owner, coll, "rkey", map[string]any{"x": 1})
	require.NoError(t, err)

	err = s.DeleteRecord(t.Context(), uri, coll, "rkey")
	require.NoError(t, err)

	_, err = s.GetRecord(t.Context(), uri, owner, coll, "rkey")
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestDeleteRecord_Nonexistent(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	// Deleting a nonexistent record should not error
	err = s.DeleteRecord(t.Context(), uri, syntax.NSID("network.habitat.note"), "nonexistent")
	require.NoError(t, err)
}

func TestSpaceURI(t *testing.T) {
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "my-key")
	require.Equal(t, "ats://did:plc:owner/network.habitat.group/my-key", uri.String())
	require.Equal(t, owner, uri.SpaceDID())
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
