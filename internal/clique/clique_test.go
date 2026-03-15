package clique

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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
	owner = syntax.DID("did:plc:owner")
	alice = syntax.DID("did:plc:alice")
	bob   = syntax.DID("did:plc:bob")
)

func TestCreateClique(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{alice, bob})
	require.NoError(t, err)
	require.NotEmpty(t, clique)
}

func TestGetMembers(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{alice, bob})
	require.NoError(t, err)

	members, err := s.GetMembers(clique)
	require.NoError(t, err)
	// owner is always added as a member
	require.ElementsMatch(t, []syntax.DID{owner, alice, bob}, members)
}

func TestGetMembers_Empty(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{})
	require.NoError(t, err)

	members, err := s.GetMembers(clique)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{owner}, members)
}

func TestAddMembers(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{alice})
	require.NoError(t, err)

	err = s.AddMembers(clique, []syntax.DID{bob})
	require.NoError(t, err)

	members, err := s.GetMembers(clique)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{owner, alice, bob}, members)
}

func TestAddMembers_Idempotent(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{alice})
	require.NoError(t, err)

	err = s.AddMembers(clique, []syntax.DID{alice})
	require.NoError(t, err)

	members, err := s.GetMembers(clique)
	require.NoError(t, err)
	require.ElementsMatch(t, []syntax.DID{owner, alice}, members)
}

func TestAddMembers_CliqueNotFound(t *testing.T) {
	s := newTestStore(t)

	nonexistent := habitat_syntax.ConstructClique(owner, "nonexistent-key")
	err := s.AddMembers(nonexistent, []syntax.DID{bob})
	require.ErrorIs(t, err, ErrCliqueNotFound)
}

func TestIsMember_True(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{alice})
	require.NoError(t, err)

	isMember, err := s.IsMember(clique, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_OwnerIsAlwaysMember(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{})
	require.NoError(t, err)

	isMember, err := s.IsMember(clique, owner)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestIsMember_False(t *testing.T) {
	s := newTestStore(t)

	clique, err := s.CreateClique(owner, []syntax.DID{alice})
	require.NoError(t, err)

	isMember, err := s.IsMember(clique, bob)
	require.NoError(t, err)
	require.False(t, isMember)
}
