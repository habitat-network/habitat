package simplespace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/schema"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
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
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.createSpace",
		strings.NewReader(`{"type": "network.habitat.group"}`),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	err := json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
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
			body, err := json.Marshal(habitat.NetworkHabitatSimplespaceCreateSpaceInput{
				Did:  tt.did,
				Type: "network.habitat.group",
			})
			require.NoError(t, err)
			req := httptest.NewRequest(
				http.MethodPost,
				"/xrpc/network.habitat.simplespace.createSpace",
				strings.NewReader(string(body)),
			)
			w := httptest.NewRecorder()
			s.CreateSpace(w, req)

			require.Equal(t, tt.want, w.Code)
			if tt.wantErr != "" {
				var apiErr atclient.ErrorBody
				err := json.NewDecoder(w.Body).Decode(&apiErr)
				require.NoError(t, err)
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

	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.createSpace",
		strings.NewReader(`{"type": "network.habitat.group", "skey": "dup"}`),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var apiErr atclient.ErrorBody
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	require.Equal(t, "SpaceAlreadyExists", apiErr.Name)
}

func TestServer_AddMember(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.addMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.AddMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	isMember, err := s.store.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestServer_AddMember_SpaceNotFound(t *testing.T) {
	s := newTestServer(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.addMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.AddMember(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var apiErr atclient.ErrorBody
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}

func TestServer_RemoveMember(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.removeMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

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

	body := `{"space": "` + uri.String() + `", "did": "` + orgID.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.removeMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListMembers(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.simplespace.listMembers?space="+uri.String(),
		http.NoBody,
	)
	w := httptest.NewRecorder()
	s.ListMembers(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSimplespaceListMembersOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)

	var dids []string
	for _, m := range output.Members {
		dids = append(dids, m.Did)
	}
	require.ElementsMatch(t, []string{owner.String(), alice.String(), orgID.String()}, dids)
}

func TestServer_Unauthorized(t *testing.T) {
	s := newTestServer(t, WithValidator(authntest.NewFailureValidator()))

	body := `{"type": "network.habitat.group"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.createSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
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

	// createSpace
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.createSpace",
		strings.NewReader(`{"type": "network.habitat.group", "skey": "shared"}`),
	)
	createW := httptest.NewRecorder()
	s.CreateSpace(createW, createReq)
	require.Equal(t, http.StatusOK, createW.Code)

	var createOutput habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	require.NoError(t, json.NewDecoder(createW.Body).Decode(&createOutput))
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
	addBody := `{"space": "` + uri.String() + `", "did": "` + alice.String() + `"}`
	addReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.addMember",
		strings.NewReader(addBody),
	)
	addW := httptest.NewRecorder()
	s.AddMember(addW, addReq)
	require.Equal(t, http.StatusOK, addW.Code)

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
	removeBody := `{"space": "` + uri.String() + `", "did": "` + alice.String() + `"}`
	removeReq := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.removeMember",
		strings.NewReader(removeBody),
	)
	removeW := httptest.NewRecorder()
	s.RemoveMember(removeW, removeReq)
	require.Equal(t, http.StatusOK, removeW.Code)

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

	body := `{"space": "` + uri.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.deleteSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteSpace(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	_, err = s.store.spaces.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestServer_DeleteSpace_SpaceNotFound(t *testing.T) {
	s := newTestServer(t)

	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")
	body := `{"space": "` + uri.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.simplespace.deleteSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteSpace(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	var apiErr atclient.ErrorBody
	require.NoError(t, json.NewDecoder(w.Body).Decode(&apiErr))
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}
