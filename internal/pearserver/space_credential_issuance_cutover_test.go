package pearserver_test

import (
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/api/habitat"
	client_test "github.com/habitat-network/habitat/internal/clientmetadata/testutil"
	http_test "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/keyval"
	pear_test "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestLegacyCredentialIssuanceStopsBeforeSideEffects(t *testing.T) {
	ts := pear_test.NewTestServer(t)
	ctx := t.Context()
	marked, _ := cutoverSpace(t, ts, "cutover", groupTp)
	staged, _ := cutoverSpace(t, ts, "staged", groupTp)
	legacy, _ := cutoverSpace(t, ts, "legacy", groupTp)
	clientID, attester := client_test.AttestationTestClient(t, "key-1")
	grantAppAccess(t, ts.SpaceStore, legacy.SpaceOwner(), clientID)
	var callbacks atomic.Int32
	endpoint := confirmingEndpoint(t, func(c string) string {
		callbacks.Add(1)
		return c
	})
	for _, uri := range []habitat_syntax.SpaceURI{marked, staged, legacy} {
		_, err := ts.PermStore.SetUserRelation(ctx, owner, uri, habitat_syntax.SpaceRoleReader)
		require.NoError(t, err)
	}
	markCutover(t, ts, marked)
	require.NoError(t, spaces.SetGroupSpacePolicy(ctx, ts.SpaceStore, staged,
		spaces.Policy{Type: spaces.PolicyAtgrp}))
	kv, err := keyval.New(ctx, ts.DB)
	require.NoError(t, err)
	spent := kv.Namespace("space_delegation_spent")

	for _, uri := range []habitat_syntax.SpaceURI{marked, staged, legacy} {
		for _, withNotify := range []bool{false, true} {
			token := delegationToken(t, ts, owner, uri)
			kid := credentialKid(t)
			attestation := client_test.SignAttestation(
				t, attester, "key-1", clientID, uri.SpaceOwner(), nil,
			)
			client := http_test.NewTestXRPCClient(t)
			var code int
			var body json.RawMessage
			if withNotify {
				code = client.Procedure(ts.Server.AuthorizeSpaceSync,
					comatproto.ComAtprotoSpaceAuthorizeSpaceSyncInput{
						Space: uri.String(), CredentialKey: kid, DelegationToken: token,
						ClientAttestation: attestation,
						Notify: &comatproto.ComAtprotoSpaceAuthorizeSpaceSyncNotify{
							Endpoint: endpoint.URL, Token: "test",
						},
					}, &body)
			} else {
				code = client.Procedure(ts.Server.GetSpaceCredential,
					habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
						Space: uri.String(), CredentialKid: kid, DelegationToken: token,
						ClientAttestation: attestation,
					}, &body)
			}
			claims := jwt.MapClaims{}
			_, _, err := jwt.NewParser().ParseUnverified(token, claims)
			require.NoError(t, err)
			var used bool
			err = spent.Get(ctx, owner.String()+" "+claims["jti"].(string), &used)
			if uri != legacy {
				require.ErrorIs(t, err, keyval.ErrNotFound)
				require.Equal(t, http.StatusBadRequest, code, string(body))
				require.Contains(t, string(body), `"SpaceNotFound"`)
				require.NotContains(t, string(body), `"credential"`)
			} else {
				require.NoError(t, err)
				require.True(t, used)
				require.Equal(t, http.StatusOK, code, string(body))
				require.Contains(t, string(body), `"credential"`)
			}
		}
		hooks, err := ts.NotifyStore.ListWebhooks(ctx, uri)
		require.NoError(t, err)
		if uri == legacy {
			require.EqualValues(t, 1, callbacks.Load())
			require.Len(t, hooks, 1)
		} else {
			require.Zero(t, callbacks.Load())
			require.Empty(t, hooks)
		}
	}
}
