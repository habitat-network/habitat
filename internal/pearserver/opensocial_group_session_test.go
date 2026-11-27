package pearserver_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	opensocial_api "github.com/habitat-network/habitat/api/opensocial"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/opensocial"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_GroupSessionNarrowing(t *testing.T) {
	adminTS, _, _ := newSharedOpenSocialServers(t)
	org, err := adminTS.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	group := syntax.DID(org)
	store := adminTS.OpenSocialStore
	client := httpx_testutil.NewTestXRPCClient(t)

	// session is an OAuth session acting as the group, approved by authorizer.
	session := func(authorizer syntax.DID, scopes ...string) *pearserver_testutil.TestServer {
		return pearserver_testutil.NewTestServer(t,
			pearserver_testutil.WithValidator(authntest.NewSuccessValidator(&authn.CredentialInfo{
				Subject: group, Method: authn.ValidatorMethodOAuth, Authorizer: authorizer,
				Scopes: scopes,
			})),
			pearserver_testutil.WithDB(adminTS.DB),
			pearserver_testutil.WithFGA(adminTS.FGA),
			pearserver_testutil.WithSpaceStore(adminTS.SpaceStore),
		)
	}
	listRequests := func(ts *pearserver_testutil.TestServer) int {
		var out opensocial_api.OrgAtgrpListJoinRequestsOutput
		return client.Query(ts.Server.GroupListJoinRequests, nil, &out)
	}
	createSpace := func(ts *pearserver_testutil.TestServer, skey string) int {
		var out comatproto.ComAtprotoSpaceCreateSpaceOutput
		return client.Procedure(ts.Server.CreateSpace, comatproto.ComAtprotoSpaceCreateSpaceInput{
			SpaceType: "network.habitat.docs", Skey: skey,
			Policy: map[string]any{"$type": spaces.PolicyGroupSpace},
		}, &out)
	}

	t.Run("a session follows its authorizer's current roles", func(t *testing.T) {
		ts := session(admin, "atproto")
		require.Equal(t, http.StatusOK, listRequests(ts))
		require.Equal(t, http.StatusOK, createSpace(ts, "one"))

		require.NoError(t, store.AssignRoles(t.Context(), group, alice, []string{
			opensocial.MemberRoleRkey, opensocial.AdminRoleRkey,
		}))
		require.NoError(t, store.AssignRoles(
			t.Context(), group, admin, []string{opensocial.MemberRoleRkey},
		))
		require.Equal(t, http.StatusUnauthorized, listRequests(ts), "admin was demoted")
		require.Equal(t, http.StatusUnauthorized, createSpace(ts, "two"))
		require.Equal(t, http.StatusOK, listRequests(session(alice, "atproto")))
	})

	t.Run("a token's own scopes narrow the session further", func(t *testing.T) {
		ts := session(alice, "atproto", "rpc:org.atgrp.listJoinRequests")
		require.Equal(t, http.StatusOK, listRequests(ts))
		require.Equal(t, http.StatusUnauthorized, createSpace(ts, "three"))
	})

	t.Run("a session issued before authorizers were kept acts as the group", func(t *testing.T) {
		ts := session("", "atproto")
		require.Equal(t, http.StatusOK, listRequests(ts))
		require.Equal(t, http.StatusOK, createSpace(ts, "legacy"))
	})

	t.Run("direct record writes follow the same rules", func(t *testing.T) {
		members := habitat_syntax.ConstructSpaceURI(group, opensocial.MembersSpaceType, "self")
		put := func(ts *pearserver_testutil.TestServer, collection, rkey string, record map[string]any) int {
			var out struct{}
			return client.Procedure(ts.Server.PutRecord, map[string]any{
				"space": members.String(), "repo": group.String(), "collection": collection,
				"rkey": rkey, "record": record,
			}, &out)
		}
		ts := session(alice, "atproto")
		require.Equal(
			t,
			http.StatusForbidden,
			put(
				ts,
				opensocial.SpaceIndexCollection,
				"x",
				map[string]any{"$type": opensocial.SpaceIndexCollection},
			),
			"the host maintains the index",
		)
		require.Equal(t, http.StatusOK, put(ts, opensocial.MembershipCollection, string(alice),
			map[string]any{
				"$type": opensocial.MembershipCollection, "createdAt": "2026-01-01T00:00:00Z",
				"roles": []string{opensocial.MemberRoleRkey, opensocial.AdminRoleRkey},
			}))
		require.Equal(
			t,
			http.StatusBadRequest,
			put(ts, opensocial.MembershipCollection, string(alice),
				map[string]any{
					"$type": opensocial.MembershipCollection, "createdAt": "2026-01-01T00:00:00Z",
					"roles": []string{opensocial.MemberRoleRkey},
				}),
			"alice is the only admin now that admin was demoted",
		)
	})
}
