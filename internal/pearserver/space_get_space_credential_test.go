package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	clientmetadata_testutil "github.com/habitat-network/habitat/internal/clientmetadata/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// grantAppAccess writes an appAccess record for clientID directly into
// uri's owner repo, putting the space in allow-list mode for it — the way
// the (not yet reintroduced) AddAppAccess handler would.
func grantAppAccess(
	t *testing.T,
	store spaces.Store,
	uri habitat_syntax.SpaceURI,
	clientID string,
) {
	t.Helper()
	rkey, err := habitat_syntax.AppAccessRkey(clientID)
	require.NoError(t, err)
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{})
	require.NoError(t, err)
	_, _, err = store.PutRecord(
		t.Context(), uri, uri.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey, recordBytes,
	)
	require.NoError(t, err)
}

func TestServer_GetSpaceCredential(t *testing.T) {
	t.Run("open space", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t)
		uri, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "test")
		require.NoError(t, err)

		t.Run("no attestation is accepted", func(t *testing.T) {
			var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential,
				habitat.NetworkHabitatSpaceGetSpaceCredentialInput{Space: uri.String()},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.NotEmpty(t, out.Credential)
		})

		t.Run("valid attestation is accepted", func(t *testing.T) {
			clientID, priv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
			attestation := clientmetadata_testutil.SignAttestation(
				t,
				priv,
				"key-1",
				clientID,
				org,
				nil,
			)

			var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential,
				habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
					Space: uri.String(), ClientAttestation: attestation,
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.NotEmpty(t, out.Credential)
		})
	})

	t.Run("allow-listed space", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t)
		uri, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "test")
		require.NoError(t, err)

		// GetSpaceCredential verifies the attestation's aud against the space
		// owner DID, which org's CreateSpace call above makes owner==org.
		clientID, priv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
		grantAppAccess(t, ts.SpaceStore, uri, clientID)

		t.Run("no attestation is rejected", func(t *testing.T) {
			var apiErr atclient.ErrorBody
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential,
				habitat.NetworkHabitatSpaceGetSpaceCredentialInput{Space: uri.String()},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidClientAttestation", apiErr.Name)
		})

		t.Run("granted client with valid attestation is accepted", func(t *testing.T) {
			attestation := clientmetadata_testutil.SignAttestation(
				t,
				priv,
				"key-1",
				clientID,
				org,
				nil,
			)
			var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential,
				habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
					Space: uri.String(), ClientAttestation: attestation,
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.NotEmpty(t, out.Credential)
		})

		t.Run("non-granted client is rejected", func(t *testing.T) {
			otherClientID, otherPriv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
			attestation := clientmetadata_testutil.SignAttestation(
				t,
				otherPriv,
				"key-1",
				otherClientID,
				org,
				nil,
			)
			var apiErr atclient.ErrorBody
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential,
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
				ts.Server.GetSpaceCredential,
				habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
					Space: uri.String(), ClientAttestation: "not-a-jwt",
				},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidClientAttestation", apiErr.Name)
		})
	})
}
