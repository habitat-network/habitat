package pearserver_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TestServer_Simplespace covers the network.habitat.simplespace.* endpoints
// with a single shared TestServer (caller = owner of org), giving each subtest
// its own distinctly-keyed space so they don't interfere on the shared store.
func TestServer_Simplespace(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t)
	client := httpx_testutil.NewTestXRPCClient(t)

	// createGroupSpace reuses the simplespace CreateSpace handler via the store
	// (org/owner), so a subtest can seed a space directly with a distinct key.
	createGroupSpace := func(t *testing.T, skey string) habitat_syntax.SpaceURI {
		t.Helper()
		uri, err := ts.SimpleStore.CreateSpace(
			t.Context(), org, owner, groupTp, habitat_syntax.SpaceKey(skey),
		)
		require.NoError(t, err)
		return uri
	}

	t.Run("create space", func(t *testing.T) {
		t.Run("creates a space with an auto key", func(t *testing.T) {
			var output habitat.NetworkHabitatSimplespaceCreateSpaceOutput
			code := client.Procedure(
				ts.Server.CreateSpace,
				habitat.NetworkHabitatSimplespaceCreateSpaceInput{Type: "network.habitat.group"},
				&output,
			)
			require.Equal(t, http.StatusOK, code)
			require.Contains(
				t,
				output.Uri,
				"at://did:plc:org/space/network.habitat.group/",
			)
		})

		t.Run("accepts only caller did or org as the did param", func(t *testing.T) {
			tests := []struct {
				name    string
				did     string
				want    int
				wantErr string
			}{
				{name: "caller did", did: owner.String(), want: http.StatusOK},
				{name: "caller org", did: org.String(), want: http.StatusOK},
				{
					name:    "other did",
					did:     alice.String(),
					want:    http.StatusBadRequest,
					wantErr: "only caller did or caller org are allowed",
				},
			}
			for _, tt := range tests {
				t.Run(tt.name, func(t *testing.T) {
					var out json.RawMessage
					code := client.Procedure(
						ts.Server.CreateSpace,
						habitat.NetworkHabitatSimplespaceCreateSpaceInput{
							Did:  tt.did,
							Type: "network.habitat.group",
						},
						&out,
					)
					require.Equal(t, tt.want, code)
					if tt.wantErr != "" {
						var apiErr atclient.ErrorBody
						require.NoError(t, json.Unmarshal(out, &apiErr))
						require.Equal(t, tt.wantErr, apiErr.Message)
					}
				})
			}
		})

		// Duplicate pins the createSpace endpoint's duplicate handling: it must
		// report the collision as a 400 SpaceAlreadyExists rather than a 500,
		// which requires the handler to translate the store's
		// spaces.ErrSpaceAlreadyExists into the local ErrSpaceAlreadyExists that
		// this handler's errors.Is check actually matches against.
		t.Run("reports a duplicate as SpaceAlreadyExists", func(t *testing.T) {
			_, err := ts.SimpleStore.CreateSpace(t.Context(), org, owner, groupTp, "dup")
			require.NoError(t, err)

			var apiErr atclient.ErrorBody
			code := client.Procedure(
				ts.Server.CreateSpace,
				habitat.NetworkHabitatSimplespaceCreateSpaceInput{
					Type: "network.habitat.group",
					Skey: "dup",
				},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "SpaceAlreadyExists", apiErr.Name)
		})
	})

	t.Run("members", func(t *testing.T) {
		t.Run("adds a member", func(t *testing.T) {
			uri := createGroupSpace(t, "add-shared")
			var out struct{}
			code := client.Procedure(
				ts.Server.AddMember,
				habitat.NetworkHabitatSimplespaceAddMemberInput{
					Space: uri.String(),
					Did:   "did:plc:alice",
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)

			isMember, err := ts.SimpleStore.IsMember(t.Context(), org, uri, alice)
			require.NoError(t, err)
			require.True(t, isMember)
		})

		t.Run("add fails for a missing space", func(t *testing.T) {
			uri := habitat_syntax.ConstructSpaceURI(owner, groupTp, "nonexistent")
			var apiErr atclient.ErrorBody
			code := client.Procedure(
				ts.Server.AddMember,
				habitat.NetworkHabitatSimplespaceAddMemberInput{
					Space: uri.String(),
					Did:   "did:plc:alice",
				},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "SpaceNotFound", apiErr.Name)
		})

		t.Run("removes a member", func(t *testing.T) {
			uri := createGroupSpace(t, "remove-shared")
			err := ts.SimpleStore.AddMember(t.Context(), uri, alice)
			require.NoError(t, err)

			var out struct{}
			code := client.Procedure(
				ts.Server.RemoveMember,
				habitat.NetworkHabitatSimplespaceRemoveMemberInput{
					Space: uri.String(),
					Did:   "did:plc:alice",
				},
				&out,
			)
			require.Equal(t, http.StatusOK, code)

			isMember, err := ts.SimpleStore.IsMember(t.Context(), org, uri, alice)
			require.NoError(t, err)
			require.False(t, isMember)
		})

		// CannotRemoveOrg pins the removeMember endpoint's 400 mapping for
		// ErrCannotRemoveOrg: the space's own org can never be removed as a
		// member, even by a request that's otherwise authorized to manage
		// members.
		t.Run("cannot remove the space's own org", func(t *testing.T) {
			uri := createGroupSpace(t, "remove-org")
			var out struct{}
			code := client.Procedure(
				ts.Server.RemoveMember,
				habitat.NetworkHabitatSimplespaceRemoveMemberInput{
					Space: uri.String(),
					Did:   org.String(),
				},
				&out,
			)
			require.Equal(t, http.StatusBadRequest, code)
		})

		t.Run("lists members", func(t *testing.T) {
			uri := createGroupSpace(t, "list-shared")
			err := ts.SimpleStore.AddMember(t.Context(), uri, alice)
			require.NoError(t, err)

			var output habitat.NetworkHabitatSimplespaceListMembersOutput
			code := client.Query(
				ts.Server.ListMembers,
				url.Values{"space": []string{uri.String()}},
				&output,
			)
			require.Equal(t, http.StatusOK, code)

			var dids []string
			for _, m := range output.Members {
				dids = append(dids, m.Did)
			}
			require.ElementsMatch(t, []string{owner.String(), alice.String(), org.String()}, dids)
		})
	})

	t.Run("space lifecycle", func(t *testing.T) {
		// SpaceLifecycle exercises the full member lifecycle end-to-end:
		// creating a space, writing a record into it, listing it back, adding a
		// second member who also writes, and removing that member again. It pins
		// that RemoveMember only revokes the permission grant — it does not touch
		// the space's stored repos, so ListRepos still shows both writers after
		// the removal.
		coll := syntax.NSID("network.habitat.note")

		var createOutput habitat.NetworkHabitatSimplespaceCreateSpaceOutput
		createCode := client.Procedure(
			ts.Server.CreateSpace,
			habitat.NetworkHabitatSimplespaceCreateSpaceInput{
				Type: "network.habitat.group", Skey: "lifecycle-shared",
			},
			&createOutput,
		)
		require.Equal(t, http.StatusOK, createCode)
		uri := habitat_syntax.SpaceURI(createOutput.Uri)

		// PutRecord in the space, as the owner.
		_, _, err := ts.SpaceStore.PutRecord(
			t.Context(),
			uri,
			owner,
			coll,
			"k1",
			spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
		)
		require.NoError(t, err)

		// It shows up in ListSpaces.
		spaceURIs, err := ts.SpaceStore.ListSpaces(t.Context(), owner, nil, nil)
		require.NoError(t, err)
		require.Contains(t, spaceURIs, uri)

		// Add someone to the space.
		var addOut struct{}
		addCode := client.Procedure(
			ts.Server.AddMember,
			habitat.NetworkHabitatSimplespaceAddMemberInput{
				Space: uri.String(),
				Did:   alice.String(),
			},
			&addOut,
		)
		require.Equal(t, http.StatusOK, addCode)

		// Alice writes into the space too, so she shows up as a repo.
		_, _, err = ts.SpaceStore.PutRecord(
			t.Context(),
			uri,
			alice,
			coll,
			"k1",
			spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 2}),
		)
		require.NoError(t, err)

		// ListRepos now shows both members.
		repos, err := ts.SpaceStore.ListRepos(t.Context(), uri)
		require.NoError(t, err)
		var repoDIDs []syntax.DID
		for _, r := range repos {
			repoDIDs = append(repoDIDs, r.DID)
		}
		require.ElementsMatch(t, []syntax.DID{owner, alice, org}, repoDIDs)

		// Remove that member from the space.
		var removeOut struct{}
		removeCode := client.Procedure(
			ts.Server.RemoveMember,
			habitat.NetworkHabitatSimplespaceRemoveMemberInput{
				Space: uri.String(),
				Did:   alice.String(),
			},
			&removeOut,
		)
		require.Equal(t, http.StatusOK, removeCode)

		isMember, err := ts.SimpleStore.IsMember(t.Context(), org, uri, alice)
		require.NoError(t, err)
		require.False(t, isMember)

		// ListRepos still shows that member: removing a member revokes their
		// permission grant, but the repo they already wrote to is untouched.
		repos, err = ts.SpaceStore.ListRepos(t.Context(), uri)
		require.NoError(t, err)
		repoDIDs = nil
		for _, r := range repos {
			repoDIDs = append(repoDIDs, r.DID)
		}
		require.ElementsMatch(t, []syntax.DID{owner, alice, org}, repoDIDs)
	})

	t.Run("delete space", func(t *testing.T) {
		t.Run("deletes a space", func(t *testing.T) {
			uri := createGroupSpace(t, "to-delete")
			err := ts.SimpleStore.AddMember(t.Context(), uri, alice)
			require.NoError(t, err)

			var out struct{}
			code := client.Procedure(
				ts.Server.DeleteSpace,
				habitat.NetworkHabitatSimplespaceDeleteSpaceInput{Space: uri.String()},
				&out,
			)
			require.Equal(t, http.StatusOK, code)

			_, err = ts.SpaceStore.ListRepos(t.Context(), uri)
			require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
		})

		t.Run("fails for a missing space", func(t *testing.T) {
			uri := habitat_syntax.ConstructSpaceURI(owner, groupTp, "nonexistent")
			var apiErr atclient.ErrorBody
			code := client.Procedure(
				ts.Server.DeleteSpace,
				habitat.NetworkHabitatSimplespaceDeleteSpaceInput{Space: uri.String()},
				&apiErr,
			)
			require.Equal(t, http.StatusBadRequest, code)
			require.Equal(t, "SpaceNotFound", apiErr.Name)
		})
	})
}

// TestServer_Simplespace_Unauthorized uses a failing validator, so it needs its
// own server.
func TestServer_Simplespace_Unauthorized(t *testing.T) {
	ts := pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithValidator(authntest.NewFailureValidator()),
	)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		ts.Server.CreateSpace,
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{Type: "network.habitat.group"},
		&out,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}
