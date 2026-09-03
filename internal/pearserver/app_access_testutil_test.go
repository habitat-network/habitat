package pearserver

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// GrantAppAccess writes an appAccess record for clientID directly into
// uri's owner repo, putting the space in allow-list mode for it — the way
// the (not yet reintroduced) AddAppAccess handler would. Exported for
// internal/pearserver_test, which needs appAccessRkey's exact derivation to
// be visible from the package it's defined in.
func GrantAppAccess(
	t *testing.T,
	store spaces.Store,
	uri habitat_syntax.SpaceURI,
	clientID string,
) {
	t.Helper()
	rkey, err := appAccessRkey(clientID)
	require.NoError(t, err)
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{})
	require.NoError(t, err)
	_, _, err = store.PutRecord(
		t.Context(), uri, uri.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey, recordBytes,
	)
	require.NoError(t, err)
}
