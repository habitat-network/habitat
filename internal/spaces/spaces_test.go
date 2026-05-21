package spaces

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestStore(t *testing.T) Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	s, err := NewStore(db)
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

	uri1, err := s.CreateSpace(t.Context(), owner, groupType, "space1")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), owner, groupType, "space2")
	require.NoError(t, err)

	// Owner should see both
	spaces, err := s.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 2)

	// uri1 should appear in the results
	uris := make([]SpaceURI, len(spaces))
	for i, sp := range spaces {
		uris[i] = sp.URI
	}
	require.Contains(t, uris, uri1)

	// Alice should see none (not the owner)
	spaces, err = s.ListSpaces(t.Context(), alice, nil, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 0)
}

func TestListSpaces_FilterByType(t *testing.T) {
	s := newTestStore(t)

	personal := syntax.NSID("network.habitat.personal")

	_, err := s.CreateSpace(t.Context(), owner, groupType, "group1")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), owner, personal, "personal1")
	require.NoError(t, err)

	spaces, err := s.ListSpaces(t.Context(), owner, &groupType, nil)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, groupType, spaces[0].Type)
}

func TestListSpaces_FilterByOwner(t *testing.T) {
	s := newTestStore(t)
	other := syntax.DID("did:plc:other")

	_, err := s.CreateSpace(t.Context(), owner, groupType, "a")
	require.NoError(t, err)

	_, err = s.CreateSpace(t.Context(), other, groupType, "b")
	require.NoError(t, err)

	// Owner's spaces filtered by owner should show only their space
	spaces, err := s.ListSpaces(t.Context(), owner, nil, &owner)
	require.NoError(t, err)
	require.Len(t, spaces, 1)
	require.Equal(t, "ats://did:plc:owner/network.habitat.group/a", spaces[0].URI.String())
}

func TestGetMembers(t *testing.T) {
	s := newTestStore(t)

	uri, err := s.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	members, err := s.GetMembers(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, members, 1) // just the owner
	require.Equal(t, owner, members[0].Did)
}

func TestGetMembers_SpaceNotFound(t *testing.T) {
	s := newTestStore(t)

	uri := ConstructSpaceURI(owner, groupType, "nonexistent")
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

	uri := ConstructSpaceURI(owner, groupType, "nonexistent")
	isMember, err := s.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
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
	require.Equal(t, "my-rkey", rec.Rkey)
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

	_, err = s.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"nonexistent",
	)
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
	records, err := s.ListRecords(t.Context(), uri, nil)
	require.NoError(t, err)
	require.Len(t, records, 3)

	// Filter by collection
	records, err = s.ListRecords(t.Context(), uri, &collA)
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
	uri := ConstructSpaceURI(owner, groupType, "my-key")
	require.Equal(t, "ats://did:plc:owner/network.habitat.group/my-key", uri.String())
	require.Equal(t, owner, uri.SpaceDID())
	require.Equal(t, groupType, uri.SpaceType())
	require.Equal(t, Skey("my-key"), uri.Skey())

	parsed, err := ParseSpaceURI("ats://did:plc:owner/network.habitat.group/my-key")
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
			_, err := ParseSpaceURI(tt.input)
			require.Error(t, err)
		})
	}
}
