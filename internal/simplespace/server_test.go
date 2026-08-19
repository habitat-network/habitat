package simplespace

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/gorilla/schema"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
)

type testServerOptions struct {
	validator authn.RequestValidator
	mgr       *testManager
}

type testServer struct {
	*Server
	Manager *testManager
}

func newTestServerWithOpts(t *testing.T, opts testServerOptions) *testServer {
	t.Helper()
	if opts.validator == nil {
		opts.validator = authntest.NewSuccessValidatorWithOrg(owner, orgID)
	}
	if opts.mgr == nil {
		opts.mgr = newTestManager(t)
	}
	return &testServer{
		Server: &Server{
			validator: opts.validator,
			decoder:   schema.NewDecoder(),
			mgr:       opts.mgr.Manager,
		},
		Manager: opts.mgr,
	}
}

func TestServer_CreateSpace(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			validator: authntest.NewSuccessValidator(
				&authn.CredentialInfo{Subject: owner},
			),
		},
	)
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
		"at://did:web:everyone.example.com/space/network.habitat.group/",
	)
}

func TestServer_CreateSpaceWithDidInput(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{validator: authntest.NewSuccessValidatorWithOrg(owner, orgID)},
	)

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

func TestServer_RemoveMember(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Manager.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.Manager.AddMember(t.Context(), uri, alice)
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

	isMember, err := s.Manager.IsMember(t.Context(), orgID, uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestServer_ListMembers(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Manager.CreateSpace(t.Context(), orgID, owner, groupType, "shared")
	require.NoError(t, err)

	err = s.Manager.AddMember(t.Context(), uri, alice)
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
	s := newTestServerWithOpts(
		t,
		testServerOptions{
			validator: authntest.NewFailureValidator(),
		},
	)

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

func TestServer_DeleteSpace(t *testing.T) {
	s := newTestServerWithOpts(
		t,
		testServerOptions{},
	)

	uri, err := s.Manager.CreateSpace(t.Context(), orgID, owner, groupType, "to-delete")
	require.NoError(t, err)

	err = s.Manager.AddMember(t.Context(), uri, alice)
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

	repos, err := s.Manager.spaces.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	require.Empty(t, repos)
}
