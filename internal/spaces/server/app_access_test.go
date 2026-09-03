package spaces_server_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_AddAndRemoveAppAccess(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	const clientID = "https://app.example.com/client-metadata.json"

	var addOut habitat.NetworkHabitatSpaceAddAppAccessOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{Space: uri.String(), ClientId: clientID},
		&addOut,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, addOut.Uri)

	// appAccess records are written into the *space owner's* repo (orgID here,
	// the authority CreateSpace was called with), not the caller's own repo
	// (owner) — see internal/perms/store.go's SetUserRelation/
	// SetSpaceRoleRelation, which follow the same space.SpaceOwner() pattern.
	collection := habitat_syntax.AppAccessCollection
	records, err := store.ListRecords(t.Context(), uri, orgID, &collection)
	require.NoError(t, err)
	require.Len(t, records, 1)

	var removeOut struct{}
	code = httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.RemoveAppAccess,
		habitat.NetworkHabitatSpaceRemoveAppAccessInput{Space: uri.String(), ClientId: clientID},
		&removeOut,
	)
	require.Equal(t, http.StatusOK, code)

	records, err = store.ListRecords(t.Context(), uri, orgID, &collection)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestServer_AddAppAccess_Unauthorized(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store, WithValidator(authntest.NewFailureValidator()))
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{
			Space: uri.String(), ClientId: "https://app.example.com/client-metadata.json",
		},
		&apiErr,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestServer_AddAppAccess_SpaceNotFound(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{
			Space: uri.String(), ClientId: "https://app.example.com/client-metadata.json",
		},
		&apiErr,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}

func TestServer_AddAppAccess_InvalidClientId(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{Space: uri.String(), ClientId: "not a url"},
		&apiErr,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "InvalidClientId", apiErr.Name)
}
