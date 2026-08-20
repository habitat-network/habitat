package perms

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// rkeySafeChars matches the AT Proto record-key charset we rely on:
// base32-lowercase output ([a-z2-7]) is a strict subset of it.
var rkeySafeChars = regexp.MustCompile(`^[a-zA-Z0-9._~-]{1,512}$`)

func TestHashRkeyDiffersOnAnyPart(t *testing.T) {
	base := hashRkey("user", alice.String(), string(habitat_syntax.SpaceRoleReader))

	t.Run("different subject", func(t *testing.T) {
		got := hashRkey("user", bob.String(), string(habitat_syntax.SpaceRoleReader))
		require.NotEqual(t, base, got)
	})

	t.Run("different role", func(t *testing.T) {
		got := hashRkey("user", alice.String(), string(habitat_syntax.SpaceRoleWriter))
		require.NotEqual(t, base, got)
	})

	t.Run("different type tag", func(t *testing.T) {
		got := hashRkey("space", alice.String(), string(habitat_syntax.SpaceRoleReader))
		require.NotEqual(t, base, got)
	})
}

func TestHashRkeyIsRecordKeySafe(t *testing.T) {
	got := hashRkey("user", alice.String(), string(habitat_syntax.SpaceRoleReader))
	require.Regexp(t, rkeySafeChars, got.String())
}

func TestUserRelationRkeyDeterministic(t *testing.T) {
	got1 := userRelationRkey(alice, habitat_syntax.SpaceRoleReader)
	got2 := userRelationRkey(alice, habitat_syntax.SpaceRoleReader)
	require.Equal(t, got1, got2)

	other := userRelationRkey(alice, habitat_syntax.SpaceRoleWriter)
	require.NotEqual(t, got1, other)
}

func TestSpaceRelationRkeyDeterministic(t *testing.T) {
	group := habitat_syntax.ConstructSpaceURI(org, groupType, habitat_syntax.SpaceKey("team"))

	got1 := spaceRelationRkey(group, habitat_syntax.SpaceRoleReader, habitat_syntax.SpaceRoleReader)
	got2 := spaceRelationRkey(group, habitat_syntax.SpaceRoleReader, habitat_syntax.SpaceRoleReader)
	require.Equal(t, got1, got2)

	other := spaceRelationRkey(
		group,
		habitat_syntax.SpaceRoleReader,
		habitat_syntax.SpaceRoleWriter,
	)
	require.NotEqual(t, got1, other)
}
