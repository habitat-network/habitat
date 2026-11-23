package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

// confirmingEndpoint serves confirmRegistration, echoing the challenge unless
// echo says otherwise.
func confirmingEndpoint(t *testing.T, echo func(string) string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/com.atproto.space.confirmRegistration", r.URL.Path)
		var in struct{ Space, Token, Challenge string }
		require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
		require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
			"challenge": echo(in.Challenge),
		}))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestServerAuthorizeSpaceSyncIssuesCredentialAndRegistersWebhook(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "sync")
	require.NoError(t, err)
	endpoint := confirmingEndpoint(t, func(c string) string { return c })

	var out comatproto.ComAtprotoSpaceAuthorizeSpaceSyncOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.AuthorizeSpaceSync,
		comatproto.ComAtprotoSpaceAuthorizeSpaceSyncInput{
			Space:           uri.String(),
			CredentialKey:   credentialKid(t),
			DelegationToken: delegationToken(t, ts, owner, uri),
			Notify: &comatproto.ComAtprotoSpaceAuthorizeSpaceSyncNotify{
				Endpoint: endpoint.URL + "/", Token: "tok",
			},
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Credential)
	rev, err := ts.SpaceStore.SpaceRev(t.Context(), uri)
	require.NoError(t, err)
	require.Equal(t, rev.String(), out.SpaceRev)

	hooks, err := ts.NotifyStore.ListWebhooks(t.Context(), uri)
	require.NoError(t, err)
	require.Len(t, hooks, 1)
	require.Equal(t, endpoint.URL, hooks[0].Endpoint)
	require.Equal(t, "tok", hooks[0].Token)
}

func TestServerAuthorizeSpaceSyncRejectsUnconfirmedRegistration(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "unconfirmed")
	require.NoError(t, err)
	endpoint := confirmingEndpoint(t, func(string) string { return "wrong" })

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.AuthorizeSpaceSync,
		comatproto.ComAtprotoSpaceAuthorizeSpaceSyncInput{
			Space:           uri.String(),
			CredentialKey:   credentialKid(t),
			DelegationToken: delegationToken(t, ts, owner, uri),
			Notify: &comatproto.ComAtprotoSpaceAuthorizeSpaceSyncNotify{
				Endpoint: endpoint.URL, Token: "tok",
			},
		},
		&apiErr,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "RegistrationNotConfirmed", apiErr.Name)
	hooks, err := ts.NotifyStore.ListWebhooks(t.Context(), uri)
	require.NoError(t, err)
	require.Empty(t, hooks)
}

func TestServerAuthorizeSpaceSyncWithoutNotifyOnlyIssuesCredential(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "plain")
	require.NoError(t, err)

	var out comatproto.ComAtprotoSpaceAuthorizeSpaceSyncOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.AuthorizeSpaceSync,
		comatproto.ComAtprotoSpaceAuthorizeSpaceSyncInput{
			Space:           uri.String(),
			CredentialKey:   credentialKid(t),
			DelegationToken: delegationToken(t, ts, owner, uri),
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Credential)
}

func TestServerAuthorizeSpaceSyncRejectsBadNotify(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	uri, err := ts.SpaceStore.CreateSpace(t.Context(), owner, groupTp, "badnotify")
	require.NoError(t, err)

	for name, n := range map[string]comatproto.ComAtprotoSpaceAuthorizeSpaceSyncNotify{
		"empty token":    {Endpoint: "https://syncer.example", Token: ""},
		"long token":     {Endpoint: "https://syncer.example", Token: string(make([]byte, 513))},
		"not a url":      {Endpoint: "syncer", Token: "t"},
		"embedded creds": {Endpoint: "https://u:p@syncer.example", Token: "t"},
	} {
		t.Run(name, func(t *testing.T) {
			var apiErr atclient.ErrorBody
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				ts.Server.AuthorizeSpaceSync,
				comatproto.ComAtprotoSpaceAuthorizeSpaceSyncInput{
					Space:           uri.String(),
					CredentialKey:   credentialKid(t),
					DelegationToken: delegationToken(t, ts, owner, uri),
					Notify:          &n,
				},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "InvalidRequest", apiErr.Name)
		})
	}
}
