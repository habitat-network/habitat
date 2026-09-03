package spaces_server_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	spaces_server "github.com/habitat-network/habitat/internal/spaces/server"
)

func TestServer_GetSpaceCredential_OpenSpaceNoAttestation(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.GetSpaceCredential,
		habitat.NetworkHabitatSpaceGetSpaceCredentialInput{Space: uri.String()},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Credential)
}

func TestServer_GetSpaceCredential_AllowListSpace(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	// GetSpaceCredential verifies the attestation's aud against the space
	// owner DID, which orgID's CreateSpace call above makes owner==orgID.
	clientID, priv := spaces_server.AttestationTestClient(t, "key-1")
	spaces_server.GrantAppAccess(t, store, uri, clientID)

	t.Run("no attestation is rejected", func(t *testing.T) {
		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{Space: uri.String()},
			&apiErr,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "InvalidClientAttestation", apiErr.Name)
	})

	t.Run("granted client with valid attestation is accepted", func(t *testing.T) {
		attestation := spaces_server.SignAttestation(t, priv, "key-1", clientID, orgID, nil)
		var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), ClientAttestation: attestation,
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Credential)
	})

	t.Run("non-granted client is rejected", func(t *testing.T) {
		otherClientID, otherPriv := spaces_server.AttestationTestClient(t, "key-1")
		attestation := spaces_server.SignAttestation(
			t,
			otherPriv,
			"key-1",
			otherClientID,
			orgID,
			nil,
		)
		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), ClientAttestation: attestation,
			},
			&apiErr,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "AppNotAuthorized", apiErr.Name)
	})

	t.Run("invalid attestation is rejected", func(t *testing.T) {
		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), ClientAttestation: "not-a-jwt",
			},
			&apiErr,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "InvalidClientAttestation", apiErr.Name)
	})
}

func TestServer_GetSpaceCredential_OpenSpaceWithValidAttestation(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	clientID, priv := spaces_server.AttestationTestClient(t, "key-1")
	attestation := spaces_server.SignAttestation(t, priv, "key-1", clientID, orgID, nil)

	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.GetSpaceCredential,
		habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
			Space: uri.String(), ClientAttestation: attestation,
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Credential)
}
