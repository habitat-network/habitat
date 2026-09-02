package simplespace

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type testServerOptions struct {
	validator authn.RequestValidator
}

type Option func(*testServerOptions)

func WithValidator(v authn.RequestValidator) Option {
	return func(tso *testServerOptions) {
		tso.validator = v
	}
}

func newTestServer(t *testing.T, opts ...Option) *Server {
	store := newTestStore(t)

	options := &testServerOptions{
		validator: authntest.NewSuccessValidatorWithOrg(owner, orgID),
	}
	for _, o := range opts {
		o(options)
	}

	return &Server{
		store:     store,
		validator: options.validator,
		decoder:   schema.NewDecoder(),
	}
}

func TestServer_CreateSpace(t *testing.T) {
	s := newTestServer(t)

	var output habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.CreateSpace,
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{Type: "network.habitat.group"},
		&output,
	)

	require.Equal(t, http.StatusOK, code)
	require.Contains(
		t,
		output.Uri,
		"at://did:plc:org/space/network.habitat.group/",
	)
}

func TestServer_CreateSpaceWithDidInput(t *testing.T) {
	s := newTestServer(t)

	tests := []struct {
		name    string
		did     string
		want    int
		wantErr string
	}{
		{
			name: "caller did",
			did:  owner.String(),
			want: http.StatusOK,
		},
		{
			name: "caller org",
			did:  orgID.String(),
			want: http.StatusOK,
		},
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
			code := httpx_testutil.NewTestXRPCClient(t).Procedure(
				s.CreateSpace,
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
}

// TestServer_CreateSpace_Duplicate pins the createSpace endpoint's duplicate
// handling: it must report the collision as a 400 SpaceAlreadyExists rather
// than a 500, which requires the manager to translate the store's
// spaces.ErrSpaceAlreadyExists into the local ErrSpaceAlreadyExists that this
// handler's errors.Is check actually matches against.
func TestServer_CreateSpace_Duplicate(t *testing.T) {
	s := newTestServer(t)

	_, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "dup")
	require.NoError(t, err)

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.CreateSpace,
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{
			Type: "network.habitat.group",
			Skey: "dup",
		},
		&apiErr,
	)

	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "SpaceAlreadyExists", apiErr.Name)
}

func TestServer_AddMember(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddMember,
		habitat.NetworkHabitatSimplespaceAddMemberInput{Space: uri.String(), Did: "did:plc:alice"},
		&out,
	)
	require.Equal(t, http.StatusOK, code)

	isMember, err := s.store.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestServer_AddMember_SpaceNotFound(t *testing.T) {
	s := newTestServer(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddMember,
		habitat.NetworkHabitatSimplespaceAddMemberInput{Space: uri.String(), Did: "did:plc:alice"},
		&apiErr,
	)

	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}

func TestServer_RemoveMember(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.RemoveMember,
		habitat.NetworkHabitatSimplespaceRemoveMemberInput{
			Space: uri.String(),
			Did:   "did:plc:alice",
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)

	isMember, err := s.store.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

// TestServer_RemoveMember_CannotRemoveOrg pins the removeMember endpoint's
// 400 mapping for ErrCannotRemoveOrg: the space's own org can never be
// removed as a member, even by a request that's otherwise authorized to
// manage members.
func TestServer_RemoveMember_CannotRemoveOrg(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.RemoveMember,
		habitat.NetworkHabitatSimplespaceRemoveMemberInput{
			Space: uri.String(),
			Did:   orgID.String(),
		},
		&out,
	)
	require.Equal(t, http.StatusBadRequest, code)
}

func TestServer_ListMembers(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	var output habitat.NetworkHabitatSimplespaceListMembersOutput
	code := httpx_testutil.NewTestXRPCClient(t).Query(
		s.ListMembers,
		url.Values{"space": []string{uri.String()}},
		&output,
	)
	require.Equal(t, http.StatusOK, code)

	var dids []string
	for _, m := range output.Members {
		dids = append(dids, m.Did)
	}
	require.ElementsMatch(t, []string{owner.String(), alice.String(), orgID.String()}, dids)
}

func TestServer_Unauthorized(t *testing.T) {
	s := newTestServer(t, WithValidator(authntest.NewFailureValidator()))

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.CreateSpace,
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{Type: "network.habitat.group"},
		&out,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}

// TestServer_SpaceLifecycle exercises the full member lifecycle end-to-end:
// creating a space, writing a record into it, listing it back, adding a
// second member who also writes, and removing that member again. It pins
// that RemoveMember only revokes the permission grant — it does not touch
// the space's stored repos, so ListRepos still shows both writers after the
// removal.
func TestServer_SpaceLifecycle(t *testing.T) {
	s := newTestServer(t)
	coll := syntax.NSID("network.habitat.note")
	client := httpx_testutil.NewTestXRPCClient(t)

	// createSpace
	var createOutput habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	createCode := client.Procedure(
		s.CreateSpace,
		habitat.NetworkHabitatSimplespaceCreateSpaceInput{
			Type: "network.habitat.group", Skey: "shared",
		},
		&createOutput,
	)
	require.Equal(t, http.StatusOK, createCode)
	uri := habitat_syntax.SpaceURI(createOutput.Uri)

	// PutRecord in the space, as the owner.
	_, _, err := s.store.spaces.PutRecord(
		t.Context(),
		uri,
		owner,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 1}),
	)
	require.NoError(t, err)

	// It shows up in ListSpaces.
	spaceURIs, err := s.store.spaces.ListSpaces(t.Context(), owner, nil, nil)
	require.NoError(t, err)
	require.Contains(t, spaceURIs, uri)

	// Add someone to the space.
	var addOut struct{}
	addCode := client.Procedure(
		s.AddMember,
		habitat.NetworkHabitatSimplespaceAddMemberInput{Space: uri.String(), Did: alice.String()},
		&addOut,
	)
	require.Equal(t, http.StatusOK, addCode)

	// Alice writes into the space too, so she shows up as a repo.
	_, _, err = s.store.spaces.PutRecord(
		t.Context(),
		uri,
		alice,
		coll,
		"k1",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"x": 2}),
	)
	require.NoError(t, err)

	// ListRepos now shows both members.
	repos, err := s.store.spaces.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	var repoDIDs []syntax.DID
	for _, r := range repos {
		repoDIDs = append(repoDIDs, r.DID)
	}
	require.ElementsMatch(t, []syntax.DID{owner, alice, orgID}, repoDIDs)

	// Remove that member from the space.
	var removeOut struct{}
	removeCode := client.Procedure(
		s.RemoveMember,
		habitat.NetworkHabitatSimplespaceRemoveMemberInput{
			Space: uri.String(),
			Did:   alice.String(),
		},
		&removeOut,
	)
	require.Equal(t, http.StatusOK, removeCode)

	isMember, err := s.store.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)

	// ListRepos still shows that member: removing a member revokes their
	// permission grant, but the repo they already wrote to is untouched.
	repos, err = s.store.spaces.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	repoDIDs = nil
	for _, r := range repos {
		repoDIDs = append(repoDIDs, r.DID)
	}
	require.ElementsMatch(t, []syntax.DID{owner, alice, orgID}, repoDIDs)
}

func TestServer_DeleteSpace(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "to-delete")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	var out struct{}
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.DeleteSpace,
		habitat.NetworkHabitatSimplespaceDeleteSpaceInput{Space: uri.String()},
		&out,
	)
	require.Equal(t, http.StatusOK, code)

	_, err = s.store.spaces.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestServer_DeleteSpace_SpaceNotFound(t *testing.T) {
	s := newTestServer(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.DeleteSpace,
		habitat.NetworkHabitatSimplespaceDeleteSpaceInput{Space: uri.String()},
		&apiErr,
	)

	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}
