package pearserver_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	clientmetadata_testutil "github.com/habitat-network/habitat/internal/clientmetadata/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// grantAppAccess makes orgDID an opensocial org (by creating its members
// space, which is what verifyClientAttestation's IsOrg check keys on) and
// grants clientID app access within it, so the owner is treated as an
// allow-listed org.
func grantAppAccess(
	t *testing.T,
	store spaces.Store,
	orgDID syntax.DID,
	clientID string,
) {
	t.Helper()
	membersSpace := habitat_syntax.ConstructSpaceURI(orgDID, opensocial.MembersSpaceType, "self")
	if _, err := store.CreateSpace(
		t.Context(),
		orgDID,
		opensocial.MembersSpaceType,
		"self",
	); err != nil &&
		!errors.Is(err, spaces.ErrSpaceAlreadyExists) {
		require.NoError(t, err)
	}
	rkey, err := habitat_syntax.AppAccessRkey(clientID)
	require.NoError(t, err)
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{})
	require.NoError(t, err)
	_, _, err = store.PutRecord(
		t.Context(),
		membersSpace,
		orgDID,
		habitat_syntax.AppAccessCollection,
		rkey,
		recordBytes,
	)
	require.NoError(t, err)
}

func TestServer_GetSpaceCredential(t *testing.T) {
	t.Run("open space", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t)
		// owner is not an opensocial org, so no attestation is required.
		uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "test")
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
				owner,
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

	t.Run("org-owned space", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t)
		uri, err := ts.SpaceStore.CreateSpace(t.Context(), org, groupTp, "test")
		require.NoError(t, err)

		// GetSpaceCredential verifies the attestation's aud against the space
		// owner DID, which org's CreateSpace call above makes owner==org.
		clientID, priv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
		grantAppAccess(t, ts.SpaceStore, org, clientID)

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
