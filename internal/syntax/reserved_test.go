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

func TestGroupConfigurationSpaceTypes(t *testing.T) {
	require.True(t, habitat_syntax.IsAtgrpConfigurationSpaceType("org.atgrp.meta"))
	require.True(t, habitat_syntax.IsAtgrpConfigurationSpaceType("org.atgrp.members"))
	require.False(t, habitat_syntax.IsAtgrpConfigurationSpaceType("org.atgrp.inbox"))
	require.False(t, habitat_syntax.IsAtgrpConfigurationSpaceType("com.example.modality"))
	require.True(t, habitat_syntax.ReservedCollections.Contains("org.atgrp.space"))
	require.True(t, habitat_syntax.ReservedCollections.Contains("com.atproto.space.policy"))
}
