package pearserver_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/atgrp"
	"github.com/habitat-network/habitat/internal/authn"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestAtgrpInboxCRUD(t *testing.T) {
	seed := pearserver_testutil.NewTestServer(t)
	recipientID, err := seed.Hive.MintIdentity(t.Context(), "alice", "example")
	require.NoError(t, err)
	groupID, err := seed.Hive.MintOrgIdentity(t.Context(), "invite-test")
	require.NoError(t, err)
	alice, org := recipientID.DID, groupID.DID
	recipient := newOpenSocialServerAs(t, seed, alice, authn.ValidatorMethodServiceAuth)
	group := newOpenSocialServerAs(t, seed, org, authn.ValidatorMethodOAuth)
	remote := newOpenSocialServerAs(t, seed, "did:plc:remote", authn.ValidatorMethodServiceAuth)
	client := httpx_testutil.NewTestXRPCClient(t)
	inbox := atgrp.InboxSpace(alice)
	policy := map[string]any{"$type": "com.atproto.memberspace.policy", "publicWrite": true}
	var created comatproto.ComAtprotoSpaceCreateSpaceOutput
	input := comatproto.ComAtprotoSpaceCreateSpaceInput{
		SpaceType: atgrp.InboxSpaceType, Skey: "self", Policy: policy,
	}
	check := func(expected int, handler http.HandlerFunc, in any) {
		t.Helper()
		require.Equal(t, expected, client.Procedure(handler, in, &map[string]any{}))
	}
	require.Equal(t, http.StatusOK, client.Procedure(recipient.Server.CreateSpace,
		input, &created))
	require.Equal(t, inbox.AtSpace().String(), created.Space)
	check(http.StatusBadRequest, remote.Server.CreateSpace, input)

	check(http.StatusBadRequest, recipient.Server.UpdateSpace,
		comatproto.ComAtprotoSpaceUpdateSpaceInput{
			Space: created.Space, Policy: map[string]any{
				"$type": "com.atproto.memberspace.policy", "publicRead": true,
			},
		})
	actualPolicy, err := recipient.SpaceStore.GetSpacePolicy(t.Context(), inbox)
	require.NoError(t, err)
	require.True(t, atgrp.IsInboxPolicy(actualPolicy))

	// FGA grants do not let a group read its recipient's inbox.
	_, err = recipient.PermStore.SetUserRelation(
		t.Context(),
		org,
		inbox,
		habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)
	canRead, err := group.PermStore.CheckUserHasSpaceRole(
		t.Context(),
		org,
		inbox,
		habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)
	require.False(t, canRead)
	readers, err := group.PermStore.ListUserSubjects(
		t.Context(),
		inbox,
		habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)
	require.Equal(t, []syntax.DID{alice}, readers)

	invite := habitat.NetworkHabitatSpacePutRecordInput{
		Space: created.Space, Repo: org.String(), Collection: atgrp.InviteCollection,
		Rkey: "3jzfcijpj2z2a",
		Record: map[string]any{
			"$type": atgrp.InviteCollection, "createdAt": time.Now().UTC().Format(time.RFC3339),
		},
	}
	var result habitat.NetworkHabitatSpacePutRecordOutput
	require.Equal(t, http.StatusOK, client.Procedure(group.Server.CreateRecord, invite, &result))
	require.NotEmpty(t, result.Cid)
	_, err = atgrp.ParseBoundInvite(result.Uri, org, alice)
	require.NoError(t, err)
	check(http.StatusConflict, group.Server.CreateRecord, invite)
	check(http.StatusBadRequest, recipient.Server.CreateRecord, invite)

	update := invite
	update.Repo = "did:plc:remote"
	check(http.StatusBadRequest, remote.Server.CreateRecord, update)
	update = invite
	update.Collection = "org.atgrp.membership"
	check(http.StatusBadRequest, group.Server.PutRecord, update)
	update = invite
	update.Rkey = "self"
	check(http.StatusBadRequest, group.Server.PutRecord, update)

	list := func(repo, collection string, asGroup bool) *httptest.ResponseRecorder {
		params := url.Values{"space": {created.Space}, "repo": {repo}}
		if collection != "" {
			params.Set("collection", collection)
		}
		req := httptest.NewRequest(
			http.MethodGet,
			"/xrpc/com.atproto.space.listRecords?"+params.Encode(),
			http.NoBody,
		)
		w := httptest.NewRecorder()
		if asGroup {
			group.Server.ServeHTTP(w, req)
		} else {
			recipient.Server.ServeHTTP(w, req)
		}
		return w
	}
	require.Equal(t, http.StatusOK, list(org.String(), atgrp.InviteCollection, true).Code)
	require.Equal(t, http.StatusBadRequest, list(alice.String(), atgrp.InviteCollection, true).Code)
	require.Contains(
		t,
		list(org.String(), atgrp.InviteCollection, false).Body.String(),
		invite.Rkey,
	)

	revoke := habitat.NetworkHabitatSpaceDeleteRecordInput{
		Space:      created.Space,
		Repo:       org.String(),
		Collection: atgrp.InviteCollection,
		Rkey:       invite.Rkey,
	}
	check(http.StatusBadRequest, recipient.Server.DeleteRecord, revoke)
	check(http.StatusOK, group.Server.DeleteRecord, revoke)
	require.NotContains(
		t,
		list(org.String(), atgrp.InviteCollection, false).Body.String(),
		invite.Rkey,
	)
}
