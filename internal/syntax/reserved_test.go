package syntax_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestReservedCollectionsIncludesAppAccess(t *testing.T) {
	require.True(t, habitat_syntax.ReservedCollections.Contains(habitat_syntax.AppAccessCollection))
	require.Equal(
		t,
		"network.habitat.space.appAccess",
		string(habitat_syntax.AppAccessCollection),
	)
}
