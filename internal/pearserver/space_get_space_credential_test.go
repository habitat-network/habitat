package pearserver_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	clientmetadata_testutil "github.com/habitat-network/habitat/internal/clientmetadata/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
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

// credentialKid returns a fresh P-256 did:key to bind a credential to.
func credentialKid(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	return pub.DIDKey()
}

// delegationToken mints a delegation token for space, acting for user, signed
// by the test server's host key.
func delegationToken(
	t *testing.T,
	ts *pearserver_testutil.TestServer,
	user syntax.DID,
	space habitat_syntax.SpaceURI,
) string {
	t.Helper()
	token, err := utils.DelegationToken(ts.HostKey, user, "#habitat", space)
	require.NoError(t, err)
	return token
}

func TestServer_GetSpaceCredential(t *testing.T) {
	t.Run("open space", func(t *testing.T) {
		ts := pearserver_testutil.NewTestServer(t)
		// owner is not an opensocial org, so no attestation is required.
		uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "test")
		require.NoError(t, err)

		request := func(in habitat.NetworkHabitatSpaceGetSpaceCredentialInput, out any) int {
			return httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential, in, out,
			)
		}

		t.Run("credential is bound to credentialKid", func(t *testing.T) {
			kid := credentialKid(t)
			var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space:           uri.String(),
				CredentialKid:   kid,
				DelegationToken: delegationToken(t, ts, owner, uri),
			}, &out)
			require.Equal(t, http.StatusOK, code)
			claims := jwt.MapClaims{}
			_, _, err := jwt.NewParser().ParseUnverified(out.Credential, claims)
			require.NoError(t, err)
			require.Equal(t, map[string]any{"kid": kid}, claims["cnf"])
		})

		t.Run("missing credentialKid is rejected", func(t *testing.T) {
			var apiErr atclient.ErrorBody
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space:           uri.String(),
				DelegationToken: delegationToken(t, ts, owner, uri),
			}, &apiErr)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidRequest", apiErr.Name)
		})

		t.Run("non-P-256 credentialKid is rejected", func(t *testing.T) {
			k256, err := atcrypto.GeneratePrivateKeyK256()
			require.NoError(t, err)
			pub, err := k256.PublicKey()
			require.NoError(t, err)
			var apiErr atclient.ErrorBody
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space:           uri.String(),
				CredentialKid:   pub.DIDKey(),
				DelegationToken: delegationToken(t, ts, owner, uri),
			}, &apiErr)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidRequest", apiErr.Name)
		})

		t.Run("no delegation token answers SpaceNotFound", func(t *testing.T) {
			// Habitat has no public spaces.
			var apiErr atclient.ErrorBody
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), CredentialKid: credentialKid(t),
			}, &apiErr)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "SpaceNotFound", apiErr.Name)
		})

		t.Run("bad delegation token is rejected", func(t *testing.T) {
			var apiErr atclient.ErrorBody
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space:           uri.String(),
				CredentialKid:   credentialKid(t),
				DelegationToken: "not-a-jwt",
			}, &apiErr)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidDelegationToken", apiErr.Name)
		})

		t.Run("delegation token for another space is rejected", func(t *testing.T) {
			other := habitat_syntax.ConstructSpaceURI(owner, groupTp, "other")
			var apiErr atclient.ErrorBody
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space:           uri.String(),
				CredentialKid:   credentialKid(t),
				DelegationToken: delegationToken(t, ts, owner, other),
			}, &apiErr)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidDelegationToken", apiErr.Name)
		})

		t.Run("user without access is rejected", func(t *testing.T) {
			var apiErr atclient.ErrorBody
			code := request(habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space:           uri.String(),
				CredentialKid:   credentialKid(t),
				DelegationToken: delegationToken(t, ts, "did:web:stranger.example", uri),
			}, &apiErr)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "UserNotAuthorized", apiErr.Name)
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
					Space:             uri.String(),
					CredentialKid:     credentialKid(t),
					DelegationToken:   delegationToken(t, ts, owner, uri),
					ClientAttestation: attestation,
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

		t.Run("no attestation is accepted (public client)", func(t *testing.T) {
			// Per https://atproto.com/specs/oauth, a public client has no
			// signing key and so cannot produce a client attestation at all —
			// it isn't gated by the org's app-access allow-list.
			var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.GetSpaceCredential,
				habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
					Space:           uri.String(),
					CredentialKid:   credentialKid(t),
					DelegationToken: delegationToken(t, ts, org, uri),
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)
			require.NotEmpty(t, out.Credential)
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
					Space:             uri.String(),
					CredentialKid:     credentialKid(t),
					DelegationToken:   delegationToken(t, ts, org, uri),
					ClientAttestation: attestation,
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
					Space:             uri.String(),
					CredentialKid:     credentialKid(t),
					DelegationToken:   delegationToken(t, ts, org, uri),
					ClientAttestation: attestation,
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
					Space:             uri.String(),
					CredentialKid:     credentialKid(t),
					DelegationToken:   delegationToken(t, ts, org, uri),
					ClientAttestation: "not-a-jwt",
				},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidClientAttestation", apiErr.Name)
		})
	})
}
