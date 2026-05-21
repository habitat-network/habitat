package spaces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/fgastore"
)

// testAuth implements authn.Method for testing.
type testAuth struct{}

func (t *testAuth) CanHandle(r *http.Request) bool {
	return r.Header.Get("X-Test-DID") != ""
}

func (t *testAuth) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (syntax.DID, bool) {
	did := syntax.DID(r.Header.Get("X-Test-DID"))
	if did == "" {
		return "", false
	}
	return did, true
}

func (t *testAuth) ValidateRaw(
	ctx context.Context,
	token string,
	scopes ...string,
) (syntax.DID, bool, error) {
	did := syntax.DID(token)
	return did, did != "", nil
}

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fga, err := fgastore.NewInMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	store, err := NewStore(db, fga)
	require.NoError(t, err)
	return NewServer(store, &testAuth{}, &testAuth{})
}

func authReq(r *http.Request, did syntax.DID) *http.Request {
	r.Header.Set("X-Test-DID", did.String())
	return r
}

func TestServer_CreateSpace(t *testing.T) {
	s := newTestServer(t)

	body := `{"type": "network.habitat.group"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.createSpace",
		strings.NewReader(body),
	)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceCreateSpaceOutput
	err := json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Contains(t, output.Uri, "ats://did:plc:owner/network.habitat.group/")
}

func TestServer_ListSpaces(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "my-space")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.listSpaces", nil)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.ListSpaces(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListSpacesOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Spaces, 1)
	require.Equal(t, uri.String(), output.Spaces[0].Uri)
}

func TestServer_AddMemberAndGetMembers(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "shared")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.addMember",
		strings.NewReader(body),
	)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.AddMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getMembers?space="+uri.String(),
		nil,
	)
	req = authReq(req, owner)
	w = httptest.NewRecorder()
	s.GetMembers(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var output habitat.NetworkHabitatSpaceGetMembersOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Members, 2)

	dids := []string{output.Members[0].Did, output.Members[1].Did}
	require.Contains(t, dids, "did:plc:owner")
	require.Contains(t, dids, "did:plc:alice")
}

func TestServer_RemoveMember(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "shared")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.removeMember",
		strings.NewReader(body),
	)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	isMember, err := s.store.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestServer_PutAndGetRecord(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "collection": "network.habitat.note", "rkey": "my-note", "record": {"text": "hello"}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.putRecord",
		strings.NewReader(body),
	)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.PutRecord(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var putOutput habitat.NetworkHabitatSpacePutRecordOutput
	err = json.NewDecoder(w.Body).Decode(&putOutput)
	require.NoError(t, err)
	require.Contains(t, putOutput.Uri, "/network.habitat.note/my-note")

	getReq := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRecord?space="+uri.String()+"&collection=network.habitat.note&rkey=my-note",
		nil,
	)
	getReq = authReq(getReq, owner)
	getW := httptest.NewRecorder()
	s.GetRecord(getW, getReq)

	require.Equal(t, http.StatusOK, getW.Code)
	var getOutput habitat.NetworkHabitatSpaceGetRecordOutput
	err = json.NewDecoder(getW.Body).Decode(&getOutput)
	require.NoError(t, err)
	require.Equal(t, putOutput.Uri, getOutput.Uri)
	val, ok := getOutput.Value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", val["text"])
}

func TestServer_DeleteRecord(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "collection": "network.habitat.note", "rkey": "del-me"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteRecord",
		strings.NewReader(body),
	)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.DeleteRecord(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	_, err = s.store.GetRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"del-me",
	)
	require.ErrorIs(t, err, ErrRecordNotFound)
}

func TestServer_ListRecords(t *testing.T) {
	s := newTestServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")
	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.listRecords?space="+uri.String()+"&collection=network.habitat.note",
		nil,
	)
	req = authReq(req, owner)
	w := httptest.NewRecorder()
	s.ListRecords(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var output habitat.NetworkHabitatSpaceListRecordsOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Records, 2)
	require.Equal(t, "k1", output.Records[0].Rkey)
	require.Equal(t, "k2", output.Records[1].Rkey)
}

func TestServer_Unauthorized(t *testing.T) {
	s := newTestServer(t)

	body := `{"type": "network.habitat.group"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.createSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
