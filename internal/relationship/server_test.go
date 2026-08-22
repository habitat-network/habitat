package relationship_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/pearsetup/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// rawJSONRequest builds a request with body as its literal content, for cases
// like malformed JSON that Procedure's json.Marshal can't produce.
func rawJSONRequest(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	return req
}

var (
	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

// newTestPear returns a harness plus the org admin, who owns the spaces these
// tests create.
func newTestPear(t *testing.T) (*testutil.TestPear, *testutil.Actor) {
	t.Helper()
	p := testutil.New(t)
	return p, p.NewOrg("acme")
}

// newSpace creates a space in admin's org and grants admin the owner role on
// it, mirroring what simplespace.Store.CreateSpace does for its caller:
// spaces.Store.CreateSpace itself writes no FGA tuple, so a space's creator
// only has rights on it once one is granted explicitly.
func newSpace(
	t *testing.T,
	p *testutil.TestPear,
	admin *testutil.Actor,
	spaceType syntax.NSID,
	skey string,
) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := p.SpacesStore.CreateSpace(
		t.Context(),
		admin.Org,
		admin.DID,
		spaceType,
		habitat_syntax.SpaceKey(skey),
	)
	require.NoError(t, err)
	_, err = p.PermStore.SetUserRelation(t.Context(), admin.DID, uri, habitat_syntax.SpaceRoleOwner)
	require.NoError(t, err)
	return uri
}

func TestServer_SetUserRelation(t *testing.T) {
	// admin is the org (space owner), so it has the manager role implicitly.
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")

	var out habitat.NetworkHabitatRelationshipSetUserRelationOutput
	resp := p.Procedure(admin, "network.habitat.relationship.setUserRelation", map[string]string{
		"subject":  alice.DID.String(),
		"relation": "reader",
		"space":    space.String(),
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, out.Uri)

	allowed, err := p.PermStore.CheckUserHasSpaceRole(
		t.Context(), alice.DID, space, habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_SetUserRelation_BadBody(t *testing.T) {
	p, admin := newTestPear(t)

	req := rawJSONRequest(t, http.MethodPost,
		"/xrpc/network.habitat.relationship.setUserRelation", "{not json")
	resp := p.Do(admin, req)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_SetUserRelation_BadSpace(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")

	resp := p.Procedure(admin, "network.habitat.relationship.setUserRelation", map[string]string{
		"subject":  alice.DID.String(),
		"relation": "reader",
		"space":    "not-a-space",
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_SetUserRelation_InvalidRelation(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")

	resp := p.Procedure(admin, "network.habitat.relationship.setUserRelation", map[string]string{
		"subject":  alice.DID.String(),
		"relation": "bogus",
		"space":    space.String(),
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_SetSpaceRelation(t *testing.T) {
	p, admin := newTestPear(t)
	group := newSpace(t, p, admin, groupType, "team")
	space := newSpace(t, p, admin, docsType, "doc")

	var out habitat.NetworkHabitatRelationshipSetSpaceRelationOutput
	resp := p.Procedure(admin, "network.habitat.relationship.setSpaceRelation", map[string]string{
		"subject":     group.String(),
		"subjectRole": "reader",
		"relation":    "writer",
		"space":       space.String(),
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.NotEmpty(t, out.Uri)

	allowed, err := p.PermStore.CheckSpaceRelationHasSpaceRole(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleWriter,
	)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_SetSpaceRelation_InvalidSubjectRole(t *testing.T) {
	p, admin := newTestPear(t)
	group := newSpace(t, p, admin, groupType, "team")
	space := newSpace(t, p, admin, docsType, "doc")

	resp := p.Procedure(admin, "network.habitat.relationship.setSpaceRelation", map[string]string{
		"subject":     group.String(),
		"subjectRole": "bogus",
		"relation":    "writer",
		"space":       space.String(),
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_DeleteRelation_BadURI(t *testing.T) {
	p, admin := newTestPear(t)

	resp := p.Procedure(admin, "network.habitat.relationship.deleteRelation", map[string]string{
		"uri": "not-a-uri",
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_ListRelations(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	bob := p.NewMember(admin, "bob")
	group := newSpace(t, p, admin, groupType, "team")
	space := newSpace(t, p, admin, docsType, "doc")

	_, err := p.PermStore.SetUserRelation(t.Context(), alice.DID, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = p.PermStore.SetUserRelation(t.Context(), bob.DID, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = p.PermStore.SetSpaceRoleRelation(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	// 4, not 3: newSpace's own fixture grant (admin as owner) is a stored
	// relation too, unlike the old stub-authenticated caller, which needed no
	// such grant to pass authorization.
	var out habitat.NetworkHabitatRelationshipListRelationsOutput
	resp := p.Query(admin, "network.habitat.relationship.listRelations",
		url.Values{"space": {space.String()}}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Len(t, out.Relations, 4)

	t.Run("filters by subjectType", func(t *testing.T) {
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		resp := p.Query(admin, "network.habitat.relationship.listRelations",
			url.Values{"space": {space.String()}, "subjectType": {"user"}}, &out)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Len(t, out.Relations, 3)
	})

	t.Run("filters by relation", func(t *testing.T) {
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		resp := p.Query(admin, "network.habitat.relationship.listRelations",
			url.Values{"space": {space.String()}, "relation": {"writer"}}, &out)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Len(t, out.Relations, 1)
	})

	t.Run("filters by subjectDid", func(t *testing.T) {
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		resp := p.Query(admin, "network.habitat.relationship.listRelations",
			url.Values{"space": {space.String()}, "subjectDid": {alice.DID.String()}}, &out)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Len(t, out.Relations, 1)
	})
}

func TestServer_ListRelations_InvalidSubjectType(t *testing.T) {
	p, admin := newTestPear(t)
	space := newSpace(t, p, admin, docsType, "doc")

	resp := p.Query(admin, "network.habitat.relationship.listRelations",
		url.Values{"space": {space.String()}, "subjectType": {"clique"}}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_CheckUserRelation(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")
	_, err := p.PermStore.SetUserRelation(t.Context(), alice.DID, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipCheckUserRelationOutput
	resp := p.Query(admin, "network.habitat.relationship.checkUserRelation", url.Values{
		"space": {space.String()}, "subject": {alice.DID.String()}, "relation": {"reader"},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, out.Allowed)
}

func TestServer_CheckUserRelation_InvalidRelation(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")

	resp := p.Query(admin, "network.habitat.relationship.checkUserRelation", url.Values{
		"space": {space.String()}, "subject": {alice.DID.String()}, "relation": {"bogus"},
	}, nil)
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestServer_CheckSpaceRelation(t *testing.T) {
	p, admin := newTestPear(t)
	group := newSpace(t, p, admin, groupType, "team")
	space := newSpace(t, p, admin, docsType, "doc")
	_, err := p.PermStore.SetSpaceRoleRelation(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleWriter,
	)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipCheckSpaceRelationOutput
	resp := p.Query(admin, "network.habitat.relationship.checkSpaceRelation", url.Values{
		"space": {space.String()}, "subject": {group.String()},
		"subjectRole": {"reader"}, "relation": {"writer"},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.True(t, out.Allowed)
}

func TestServer_ResolveRelations(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")
	_, err := p.PermStore.SetUserRelation(t.Context(), alice.DID, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipResolveRelationsOutput
	resp := p.Query(admin, "network.habitat.relationship.resolveRelations", url.Values{
		"space": {space.String()}, "relation": {"reader"},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, out.Dids, alice.DID.String())
}

func TestServer_ListRelatedSpaces(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")
	_, err := p.PermStore.SetUserRelation(t.Context(), alice.DID, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipListRelatedSpacesOutput
	resp := p.Query(admin, "network.habitat.relationship.listRelatedSpaces", url.Values{
		"did": {alice.DID.String()}, "relation": {"reader"},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_ListRelatedSpaces_FiltersUnreadable(t *testing.T) {
	// bob can see his own access via the DID param but only spaces bob can read
	// are returned. bob is a reader of the space, so it is returned.
	p, admin := newTestPear(t)
	bob := p.NewMember(admin, "bob")
	space := newSpace(t, p, admin, docsType, "doc")
	_, err := p.PermStore.SetUserRelation(t.Context(), bob.DID, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	var out habitat.NetworkHabitatRelationshipListRelatedSpacesOutput
	resp := p.Query(bob, "network.habitat.relationship.listRelatedSpaces", url.Values{
		"did": {bob.DID.String()}, "relation": {"reader"},
	}, &out)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_Unauthenticated(t *testing.T) {
	p, admin := newTestPear(t)
	alice := p.NewMember(admin, "alice")
	space := newSpace(t, p, admin, docsType, "doc")

	resp := p.Query(p.Anonymous(), "network.habitat.relationship.checkUserRelation", url.Values{
		"space": {space.String()}, "subject": {alice.DID.String()}, "relation": {"reader"},
	}, nil)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// TestSetUserRelationRejectsActorWithoutSpaceRole proves an actor with no role
// on the space cannot grant one to someone else — the case the previous stub
// validator could never fail, since it discarded the authn.WithSpace(...)
// option handlers pass.
func TestSetUserRelationRejectsActorWithoutSpaceRole(t *testing.T) {
	p, admin := newTestPear(t)
	outsider := p.NewMember(admin, "outsider")
	target := p.NewMember(admin, "target")
	space := newSpace(t, p, admin, docsType, "doc")

	resp := p.Procedure(outsider, "network.habitat.relationship.setUserRelation", map[string]string{
		"subject":  target.DID.String(),
		"relation": "reader",
		"space":    space.String(),
	}, nil)
	require.NotEqual(t, http.StatusOK, resp.StatusCode,
		"a member with no role on the space must not be able to grant one")
}
