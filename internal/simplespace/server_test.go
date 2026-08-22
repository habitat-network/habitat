package simplespace_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
	"github.com/habitat-network/habitat/internal/simplespace"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var groupType = syntax.NSID("network.habitat.group")

// decodeBody decodes an error response's body. Procedure only decodes 200
// responses, so error-path assertions decode directly.
func decodeBody(resp *http.Response, out any) error {
	return json.NewDecoder(resp.Body).Decode(out)
}

// newTestPear returns a harness, its org admin, and a simplespace.Store built
// on the same underlying stores the harness's own routes use — so fixtures
// seeded through it are visible to the routed handlers, and vice versa.
func newTestPear(t *testing.T) (*testutil.TestPear, *testutil.Actor, *simplespace.Store) {
	t.Helper()
	p := testutil.New(t)
	admin := p.NewOrg("acme")
	return p, admin, simplespace.NewStore(p.DB, p.SpacesStore, p.PermStore)
}

func TestServer_CreateSpace(t *testing.T) {
	p, admin, _ := newTestPear(t)

	var out habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	resp := p.Procedure(admin, "network.habitat.simplespace.createSpace", map[string]string{
		"type": "network.habitat.group",
	}, &out)

	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, out.Uri, "at://"+admin.Org.String()+"/space/network.habitat.group/")
}

func TestServer_CreateSpaceWithDidInput(t *testing.T) {
	p, admin, _ := newTestPear(t)
	other := p.NewMember(admin, "other")

	tests := []struct {
		name    string
		did     string
		want    int
		wantErr string
	}{
		{name: "caller did", did: admin.DID.String(), want: http.StatusOK},
		{name: "caller org", did: admin.Org.String(), want: http.StatusOK},
		{
			name:    "other did",
			did:     other.DID.String(),
			want:    http.StatusBadRequest,
			wantErr: "only caller did or caller org are allowed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var apiErr atclient.ErrorBody
			resp := p.Procedure(admin, "network.habitat.simplespace.createSpace",
				habitat.NetworkHabitatSimplespaceCreateSpaceInput{
					Did:  tt.did,
					Type: "network.habitat.group",
				}, nil)
			require.Equal(t, tt.want, resp.StatusCode)
			if tt.wantErr != "" {
				require.NoError(t, decodeBody(resp, &apiErr))
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
	p, admin, ss := newTestPear(t)

	_, err := ss.CreateSpace(t.Context(), admin.Org, admin.DID, groupType, "dup")
	require.NoError(t, err)

	resp := p.Procedure(admin, "network.habitat.simplespace.createSpace", map[string]string{
		"type": "network.habitat.group",
		"skey": "dup",
	}, nil)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var apiErr atclient.ErrorBody
	require.NoError(t, decodeBody(resp, &apiErr))
	require.Equal(t, "SpaceAlreadyExists", apiErr.Name)
}

func TestServer_AddMember(t *testing.T) {
	p, admin, ss := newTestPear(t)
	alice := p.NewMember(admin, "alice")

	uri, err := ss.CreateSpace(t.Context(), admin.Org, admin.DID, groupType, "shared")
	require.NoError(t, err)

	resp := p.Procedure(admin, "network.habitat.simplespace.addMember", map[string]string{
		"space": uri.String(),
		"did":   alice.DID.String(),
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	isMember, err := ss.IsMember(t.Context(), admin.Org, uri, alice.DID)
	require.NoError(t, err)
	require.True(t, isMember)
}

func TestServer_AddMember_SpaceNotFound(t *testing.T) {
	p, admin, _ := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	uri := habitat_syntax.ConstructSpaceURI(admin.DID, groupType, "nonexistent")

	resp := p.Procedure(admin, "network.habitat.simplespace.addMember", map[string]string{
		"space": uri.String(),
		"did":   alice.DID.String(),
	}, nil)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var apiErr atclient.ErrorBody
	require.NoError(t, decodeBody(resp, &apiErr))
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}

func TestServer_RemoveMember(t *testing.T) {
	p, admin, ss := newTestPear(t)
	alice := p.NewMember(admin, "alice")

	uri, err := ss.CreateSpace(t.Context(), admin.Org, admin.DID, groupType, "shared")
	require.NoError(t, err)
	require.NoError(t, ss.AddMember(t.Context(), uri, alice.DID))

	resp := p.Procedure(admin, "network.habitat.simplespace.removeMember", map[string]string{
		"space": uri.String(),
		"did":   alice.DID.String(),
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	isMember, err := ss.IsMember(t.Context(), admin.Org, uri, alice.DID)
	require.NoError(t, err)
	require.False(t, isMember)
}

// TestServer_RemoveMember_CannotRemoveOrg pins the removeMember endpoint's
// 400 mapping for ErrCannotRemoveOrg: the space's own org can never be
// removed as a member, even by a request that's otherwise authorized to
// manage members.
func TestServer_RemoveMember_CannotRemoveOrg(t *testing.T) {
	p, admin, ss := newTestPear(t)

	uri, err := ss.CreateSpace(t.Context(), admin.Org, admin.DID, groupType, "shared")
	require.NoError(t, err)

	resp := p.Procedure(admin, "network.habitat.simplespace.removeMember", map[string]string{
		"space": uri.String(),
		"did":   admin.Org.String(),
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_ListMembers(t *testing.T) {
	p, admin, ss := newTestPear(t)
	alice := p.NewMember(admin, "alice")

	uri, err := ss.CreateSpace(t.Context(), admin.Org, admin.DID, groupType, "shared")
	require.NoError(t, err)
	require.NoError(t, ss.AddMember(t.Context(), uri, alice.DID))

	var out habitat.NetworkHabitatSimplespaceListMembersOutput
	resp := p.Query(admin, "network.habitat.simplespace.listMembers",
		url.Values{"space": {uri.String()}}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var dids []string
	for _, m := range out.Members {
		dids = append(dids, m.Did)
	}
	require.ElementsMatch(t, []string{admin.DID.String(), alice.DID.String(), admin.Org.String()}, dids)
}

func TestServer_Unauthorized(t *testing.T) {
	p, _, _ := newTestPear(t)

	resp := p.Procedure(p.Anonymous(), "network.habitat.simplespace.createSpace", map[string]string{
		"type": "network.habitat.group",
	}, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestServer_SpaceLifecycle exercises the full member lifecycle end-to-end:
// creating a space, writing a record into it, listing it back, adding a
// second member who also writes, and removing that member again. It pins
// that RemoveMember only revokes the permission grant — it does not touch
// the space's stored repos, so ListRepos still shows both writers after the
// removal.
func TestServer_SpaceLifecycle(t *testing.T) {
	p, admin, ss := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	coll := syntax.NSID("network.habitat.note")

	var createOutput habitat.NetworkHabitatSimplespaceCreateSpaceOutput
	createResp := p.Procedure(admin, "network.habitat.simplespace.createSpace", map[string]string{
		"type": "network.habitat.group",
		"skey": "shared",
	}, &createOutput)
	require.Equal(t, http.StatusOK, createResp.StatusCode)
	uri := habitat_syntax.SpaceURI(createOutput.Uri)

	// PutRecord in the space, as the owner.
	_, _, err := p.SpacesStore.PutRecord(t.Context(), uri, admin.DID, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)

	// It shows up in ListSpaces.
	spaceURIs, err := p.SpacesStore.ListSpaces(t.Context(), admin.DID, nil, nil)
	require.NoError(t, err)
	require.Contains(t, spaceURIs, uri)

	// Add someone to the space.
	addResp := p.Procedure(admin, "network.habitat.simplespace.addMember", map[string]string{
		"space": uri.String(),
		"did":   alice.DID.String(),
	}, nil)
	require.Equal(t, http.StatusOK, addResp.StatusCode)

	// Alice writes into the space too, so she shows up as a repo.
	_, _, err = p.SpacesStore.PutRecord(t.Context(), uri, alice.DID, coll, "k1", map[string]any{"x": 2})
	require.NoError(t, err)

	// ListRepos now shows both members.
	repos, err := p.SpacesStore.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	var repoDIDs []syntax.DID
	for _, r := range repos {
		repoDIDs = append(repoDIDs, r.DID)
	}
	require.ElementsMatch(t, []syntax.DID{admin.DID, alice.DID, admin.Org}, repoDIDs)

	// Remove that member from the space.
	removeResp := p.Procedure(admin, "network.habitat.simplespace.removeMember", map[string]string{
		"space": uri.String(),
		"did":   alice.DID.String(),
	}, nil)
	require.Equal(t, http.StatusOK, removeResp.StatusCode)

	isMember, err := ss.IsMember(t.Context(), admin.Org, uri, alice.DID)
	require.NoError(t, err)
	require.False(t, isMember)

	// ListRepos still shows that member: removing a member revokes their
	// permission grant, but the repo they already wrote to is untouched.
	repos, err = p.SpacesStore.ListRepos(t.Context(), uri)
	require.NoError(t, err)
	repoDIDs = nil
	for _, r := range repos {
		repoDIDs = append(repoDIDs, r.DID)
	}
	require.ElementsMatch(t, []syntax.DID{admin.DID, alice.DID, admin.Org}, repoDIDs)
}

func TestServer_DeleteSpace(t *testing.T) {
	p, admin, ss := newTestPear(t)
	alice := p.NewMember(admin, "alice")

	uri, err := ss.CreateSpace(t.Context(), admin.Org, admin.DID, groupType, "to-delete")
	require.NoError(t, err)
	require.NoError(t, ss.AddMember(t.Context(), uri, alice.DID))

	resp := p.Procedure(admin, "network.habitat.simplespace.deleteSpace", map[string]string{
		"space": uri.String(),
	}, nil)
	require.Equal(t, http.StatusOK, resp.StatusCode)

	_, err = p.SpacesStore.ListRepos(t.Context(), uri)
	require.ErrorIs(t, err, spaces.ErrSpaceNotFound)
}

func TestServer_DeleteSpace_SpaceNotFound(t *testing.T) {
	p, admin, _ := newTestPear(t)
	uri := habitat_syntax.ConstructSpaceURI(admin.DID, groupType, "nonexistent")

	resp := p.Procedure(admin, "network.habitat.simplespace.deleteSpace", map[string]string{
		"space": uri.String(),
	}, nil)

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	var apiErr atclient.ErrorBody
	require.NoError(t, decodeBody(resp, &apiErr))
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}
