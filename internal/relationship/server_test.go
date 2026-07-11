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
	"github.com/habitat-network/habitat/internal/spaces"
)

func newTestServer(t *testing.T, caller syntax.DID) (*Server, *Store, spaces.Store) {
	t.Helper()
	rel, sp := newTestStore(t)
	auth := authntest.NewSuccessMethodWithOrg(caller, caller)
	return NewServer(rel, rel.fga, auth, auth), rel, sp
}

func queryReq(path string, params url.Values) *http.Request {
	return httptest.NewRequest(http.MethodGet, path+"?"+params.Encode(), nil)
}

func TestServer_WriteTuple(t *testing.T) {
	// caller is the org (space owner), so it has the manager role implicitly.
	s, rel, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")

	body := fmt.Sprintf(
		`{"subject":{"$type":%q,"did":%q},"relation":"reader","object":{"space":%q}}`,
		subjectTypeUser, alice.String(), space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeTuple",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteTuple(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipWriteTupleOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.NotEmpty(t, out.Uri)

	allowed, err := rel.Check(t.Context(), org, UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.True(t, allowed)
}

func TestServer_WriteTuple_Unauthorized(t *testing.T) {
	s, _, sp := newTestServer(t, bob) // bob has no role on the space
	space := newSpace(t, sp, docsType, "doc")

	body := fmt.Sprintf(
		`{"subject":{"$type":%q,"did":%q},"relation":"reader","object":{"space":%q}}`,
		subjectTypeUser, alice.String(), space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeTuple",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteTuple(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_WriteTuple_InvalidSubject(t *testing.T) {
	s, _, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")

	body := fmt.Sprintf(
		`{"subject":{"$type":"network.habitat.relationship.defs#bogus"},"relation":"reader","object":{"space":%q}}`,
		space.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeTuple",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteTuple(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_DeleteTuple(t *testing.T) {
	s, rel, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")
	uri, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)

	body := fmt.Sprintf(`{"uri":%q}`, uri.String())
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.deleteTuple",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteTuple(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	allowed, err := rel.Check(t.Context(), org, UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	require.False(t, allowed)
}

func TestServer_DeleteTuple_BadURI(t *testing.T) {
	s, _, _ := newTestServer(t, org)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.deleteTuple",
		strings.NewReader(`{"uri":"not-a-uri"}`),
	)
	w := httptest.NewRecorder()
	s.DeleteTuple(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListTuples(t *testing.T) {
	s, rel, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")
	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)
	_, err = rel.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleWriter, space)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.ListTuples(w, queryReq(
		"/xrpc/network.habitat.relationship.listTuples",
		url.Values{"space": {space.String()}, "subjectType": {"user"}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipListTuplesOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Len(t, out.Tuples, 2)
}

func TestServer_ListTuples_InvalidSubjectType(t *testing.T) {
	s, _, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")

	w := httptest.NewRecorder()
	s.ListTuples(w, queryReq(
		"/xrpc/network.habitat.relationship.listTuples",
		url.Values{"space": {space.String()}, "subjectType": {"clique"}},
	))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_Check(t *testing.T) {
	s, rel, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")
	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	s.Check(w, queryReq(
		"/xrpc/network.habitat.relationship.check",
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatRelationshipCheckOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.True(t, out.Allowed)
}

func TestServer_ListSubjects(t *testing.T) {
	s, rel, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")
	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
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
	s, rel, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")
	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: alice}, RoleReader, space)
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

func TestServer_WriteTuple_BadBody(t *testing.T) {
	s, _, _ := newTestServer(t, org)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeTuple",
		strings.NewReader("{not json"),
	)
	w := httptest.NewRecorder()
	s.WriteTuple(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_WriteTuple_BadObjectSpace(t *testing.T) {
	s, _, _ := newTestServer(t, org)
	body := fmt.Sprintf(
		`{"subject":{"$type":%q,"did":%q},"relation":"reader","object":{"space":"not-a-space"}}`,
		subjectTypeUser, alice.String(),
	)
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.relationship.writeTuple",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.WriteTuple(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListTuples_Unauthorized(t *testing.T) {
	s, _, sp := newTestServer(t, bob) // bob has no role on the space
	space := newSpace(t, sp, docsType, "doc")

	w := httptest.NewRecorder()
	s.ListTuples(w, queryReq(
		"/xrpc/network.habitat.relationship.listTuples",
		url.Values{"space": {space.String()}},
	))
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_Check_InvalidRelation(t *testing.T) {
	s, _, sp := newTestServer(t, org)
	space := newSpace(t, sp, docsType, "doc")

	w := httptest.NewRecorder()
	s.Check(w, queryReq(
		"/xrpc/network.habitat.relationship.check",
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"bogus"}},
	))
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServer_ListObjects_FiltersUnreadable(t *testing.T) {
	// bob can see his own access via the DID param but only spaces bob can read
	// are returned. bob is a reader of the space, so it is returned.
	s, rel, sp := newTestServer(t, bob)
	space := newSpace(t, sp, docsType, "doc")
	_, err := rel.WriteTuple(t.Context(), UserSubject{DID: bob}, RoleReader, space)
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
	rel, sp := newTestStore(t)
	space := newSpace(t, sp, docsType, "doc")
	s := NewServer(
		rel,
		rel.fga,
		authntest.NewFailMethod(),
		authntest.NewFailMethod(),
	)

	w := httptest.NewRecorder()
	s.Check(w, queryReq(
		"/xrpc/network.habitat.relationship.check",
		url.Values{"space": {space.String()}, "subject": {alice.String()}, "relation": {"reader"}},
	))
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
