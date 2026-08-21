package relationship

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	testOrg = syntax.DID("did:plc:org")
	alice   = syntax.DID("did:plc:alice")
	bob     = syntax.DID("did:plc:bob")

	docsType  = syntax.NSID("network.habitat.docs")
	groupType = syntax.NSID("network.habitat.group")
)

// newTestServer returns a Server, along with the perms.Store and
// spaces.Store backing it, so tests can seed relations and spaces directly.
func newTestServer(t *testing.T, caller syntax.DID) (*Server, perms.Store, spaces.Store) {
	t.Helper()
	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	db := db_testutil.NewDB(t)
	sp := spaces_testutil.NewTestStore(t, spaces_testutil.Config{DB: db, FgaStore: fga})
	ps := perms.NewStore(db, sp.Store, fga)

	return NewServer(
		ps,
		sp.Store,
		authntest.NewSuccessValidatorWithOrg(caller, caller),
	), ps, sp.Store
}

// newSpace creates a space owned by org in sp and returns its URI.
func newSpace(
	t *testing.T,
	sp spaces.Store,
	spaceType syntax.NSID,
	skey string,
) habitat_syntax.SpaceURI {
	t.Helper()
	uri, err := sp.CreateSpace(
		t.Context(),
		testOrg,
		testOrg,
		spaceType,
		habitat_syntax.SpaceKey(skey),
	)
	require.NoError(t, err)
	return uri
}

func queryReq(path string, params url.Values) *http.Request {
	return httptest.NewRequest(http.MethodGet, path+"?"+params.Encode(), http.NoBody)
}

func TestServer_WriteUserRelation(t *testing.T) {
	// caller is the org (space owner), so it has the manager role implicitly.
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	body := fmt.Sprintf(
		`{"subject":%q,"relation":"reader","space":%q}`,
		alice.String(), space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeUserRelation",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteUserRelation(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipWriteUserRelationOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.Uri)

	allowed, err := ps.CheckUserHasSpaceRole(
		t.Context(), alice, space, habitat_syntax.SpaceRoleReader, testOrg,
	)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_WriteUserRelation_BadBody(t *testing.T) {
	s, _, _ := newTestServer(t, testOrg)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeUserRelation",
		strings.NewReader("{not json"),
	)
	w := httptest.NewRecorder()
	s.WriteUserRelation(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_WriteUserRelation_BadSpace(t *testing.T) {
	s, _, _ := newTestServer(t, testOrg)
	body := fmt.Sprintf(
		`{"subject":%q,"relation":"reader","space":"not-a-space"}`,
		alice.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeUserRelation",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteUserRelation(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_WriteUserRelation_InvalidRelation(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	body := fmt.Sprintf(
		`{"subject":%q,"relation":"bogus","space":%q}`,
		alice.String(), space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeUserRelation",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteUserRelation(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_WriteSpaceRelation(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")

	body := fmt.Sprintf(
		`{"subject":%q,"subjectRole":"reader","relation":"writer","space":%q}`,
		group.String(), space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeSpaceRelation",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteSpaceRelation(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipWriteSpaceRelationOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.Uri)

	allowed, err := ps.CheckSpaceRelationHasSpaceRole(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleWriter,
	)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_WriteSpaceRelation_InvalidSubjectRole(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")
	body := fmt.Sprintf(
		`{"subject":%q,"subjectRole":"bogus","relation":"writer","space":%q}`,
		group.String(), space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeSpaceRelation",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteSpaceRelation(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_DeleteRelation_BadURI(t *testing.T) {
	s, _, _ := newTestServer(t, testOrg)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.deleteRelation",
		strings.NewReader(`{"uri":"not-a-uri"}`),
	)
	w := httptest.NewRecorder()
	s.DeleteRelation(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListRelations(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.AddUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)
	_, err = ps.AddUserRelation(t.Context(), bob, space, habitat_syntax.SpaceRoleWriter)
	require.NoError(t, err)
	_, err = ps.AddSpaceRoleRelation(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleReader,
	)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.ListRelations(w, queryReq(
		"/xrpc/network.habitat.relationship.listRelations",
		url.Values{"space": {space.String()}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipListRelationsOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Len(t, out.Relations, 3)

	t.Run("filters by subjectType", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.ListRelations(w, queryReq(
			"/xrpc/network.habitat.relationship.listRelations",
			url.Values{"space": {space.String()}, "subjectType": {"user"}},
		))
		require.Equal(t, http.StatusOK, w.Code)
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
		require.Len(t, out.Relations, 2)
	})

	t.Run("filters by relation", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.ListRelations(w, queryReq(
			"/xrpc/network.habitat.relationship.listRelations",
			url.Values{"space": {space.String()}, "relation": {"writer"}},
		))
		require.Equal(t, http.StatusOK, w.Code)
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
		require.Len(t, out.Relations, 1)
	})

	t.Run("filters by subjectDid", func(t *testing.T) {
		w := httptest.NewRecorder()
		s.ListRelations(w, queryReq(
			"/xrpc/network.habitat.relationship.listRelations",
			url.Values{"space": {space.String()}, "subjectDid": {alice.String()}},
		))
		require.Equal(t, http.StatusOK, w.Code)
		var out habitat.NetworkHabitatRelationshipListRelationsOutput
		require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
		require.Len(t, out.Relations, 1)
	})
}

func TestServer_ListRelations_InvalidSubjectType(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	w := httptest.NewRecorder()
	s.ListRelations(w, queryReq(
		"/xrpc/network.habitat.relationship.listRelations",
		url.Values{"space": {space.String()}, "subjectType": {"clique"}},
	))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_CheckUserRelation(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.AddUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.CheckUserRelation(w, queryReq(
		"/xrpc/network.habitat.relationship.checkUserRelation",
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipCheckUserRelationOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.True(t, out.Allowed)
}

func TestServer_CheckUserRelation_InvalidRelation(t *testing.T) {
	s, _, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")

	w := httptest.NewRecorder()
	s.CheckUserRelation(w, queryReq(
		"/xrpc/network.habitat.relationship.checkUserRelation",
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"bogus"}},
	))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_CheckSpaceRelation(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	group := newSpace(t, sp, groupType, "team")
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.AddSpaceRoleRelation(
		t.Context(),
		group, habitat_syntax.SpaceRoleReader,
		space, habitat_syntax.SpaceRoleWriter,
	)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.CheckSpaceRelation(w, queryReq(
		"/xrpc/network.habitat.relationship.checkSpaceRelation",
		url.Values{
			"space": {space.String()}, "subject": {group.String()},
			"subjectRole": {"reader"}, "relation": {"writer"},
		},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipCheckSpaceRelationOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.True(t, out.Allowed)
}

func TestServer_ListSubjects(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.AddUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.ListSubjects(w, queryReq(
		"/xrpc/network.habitat.relationship.listSubjects",
		url.Values{"space": {space.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipListSubjectsOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Contains(t, out.Dids, alice.String())
}

func TestServer_ListObjects(t *testing.T) {
	s, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.AddUserRelation(t.Context(), alice, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.ListObjects(w, queryReq(
		"/xrpc/network.habitat.relationship.listObjects",
		url.Values{"did": {alice.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipListObjectsOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_ListObjects_FiltersUnreadable(t *testing.T) {
	// bob can see his own access via the DID param but only spaces bob can read
	// are returned. bob is a reader of the space, so it is returned.
	s, ps, sp := newTestServer(t, bob)
	space := newSpace(t, sp, docsType, "doc")
	_, err := ps.AddUserRelation(t.Context(), bob, space, habitat_syntax.SpaceRoleReader)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.ListObjects(w, queryReq(
		"/xrpc/network.habitat.relationship.listObjects",
		url.Values{"did": {bob.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipListObjectsOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Contains(t, out.Spaces, space.String())
}

func TestServer_Unauthenticated(t *testing.T) {
	_, ps, sp := newTestServer(t, testOrg)
	space := newSpace(t, sp, docsType, "doc")
	s := NewServer(ps, sp, authntest.NewFailureValidator())

	w := httptest.NewRecorder()
	s.CheckUserRelation(w, queryReq(
		"/xrpc/network.habitat.relationship.checkUserRelation",
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
