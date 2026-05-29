package spaces

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/fgastore"
)

func newTestServer(t *testing.T, oauth, serviceAuth authn.Method) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	fga, err := fgastore.NewSQLite(t.Context(), filepath.Join(t.TempDir(), "fga.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })
	store, err := NewStore(db, fga)
	require.NoError(t, err)
	return NewServer(store, fga, oauth, serviceAuth)
}

func newOwnerServer(t *testing.T) *Server {
	return newTestServer(t,
		authn.NewStubAuthnForTest(owner),
		authn.NewStubAuthnForTest(owner),
	)
}

func newAliceServer(t *testing.T) *Server {
	return newTestServer(t,
		authn.NewStubAuthnForTest(alice),
		authn.NewStubAuthnForTest(alice),
	)
}

func TestServer_CreateSpace(t *testing.T) {
	s := newOwnerServer(t)

	body := `{"type": "network.habitat.group"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.createSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.CreateSpace(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceCreateSpaceOutput
	err := json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Contains(t, output.Uri, "ats://did:plc:owner/network.habitat.group/")
}

func TestServer_ListSpaces(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "my-space")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.space.listSpaces", nil)
	w := httptest.NewRecorder()
	s.ListSpaces(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceListSpacesOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Spaces, 1)
	require.Equal(t, uri.String(), output.Spaces[0].Uri)
	require.Equal(t, output.Spaces[0].MemberCount, int64(1))
}

func TestServer_AddMemberAndGetMembers(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "shared")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice", "access": "read"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.addMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.AddMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	req = httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getMembers?space="+uri.String(),
		nil,
	)
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
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "shared")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:alice"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.removeMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	isMember, err := s.store.IsMember(t.Context(), uri, alice)
	require.NoError(t, err)
	require.False(t, isMember)
}

func TestServer_PutAndGetRecord(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "collection": "network.habitat.note", "rkey": "my-note", "record": {"text": "hello"}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.putRecord",
		strings.NewReader(body),
	)
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
	s := newOwnerServer(t)

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
	s := newOwnerServer(t)

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

func TestServer_AddMember_Unauthorized(t *testing.T) {
	s := newAliceServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:bob"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.addMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.AddMember(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_RemoveMember_Unauthorized(t *testing.T) {
	s := newAliceServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "did": "did:plc:bob"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.removeMember",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.RemoveMember(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_PutRecord_Unauthorized(t *testing.T) {
	s := newAliceServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "collection": "network.habitat.note", "rkey": "test", "record": {"x": 1}}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.putRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.PutRecord(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_DeleteRecord_Unauthorized(t *testing.T) {
	s := newAliceServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	err = s.store.PutRecord(
		t.Context(),
		uri,
		owner,
		syntax.NSID("network.habitat.note"),
		"test",
		map[string]any{"x": 1},
	)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `", "collection": "network.habitat.note", "rkey": "test"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteRecord",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteRecord(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_Unauthorized(t *testing.T) {
	s := newTestServer(t,
		authn.NewStubAuthnFailedForTest(),
		authn.NewStubAuthnFailedForTest(),
	)

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

func TestServer_DeleteSpace(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "to-delete")
	require.NoError(t, err)

	err = s.store.AddMember(t.Context(), uri, alice, SpaceAccessRead)
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteSpace(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// space should be unreachable
	_, err = s.store.GetMembers(t.Context(), uri)
	require.ErrorIs(t, err, ErrSpaceNotFound)
}

func TestServer_DeleteSpace_Unauthorized(t *testing.T) {
	s := newAliceServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	body := `{"space": "` + uri.String() + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/xrpc/network.habitat.space.deleteSpace",
		strings.NewReader(body),
	)
	w := httptest.NewRecorder()
	s.DeleteSpace(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_GetRepoOplog(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepoOplog?space="+uri.String()+"&repo=did:plc:owner",
		nil,
	)
	w := httptest.NewRecorder()
	s.GetRepoOplog(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceGetRepoOplogOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Records, 2)
	require.Equal(t, "k1", output.Records[0].Rkey)
	require.Equal(t, "k2", output.Records[1].Rkey)
	require.Equal(t, coll.String(), output.Records[0].Collection)
	require.NotEmpty(t, output.Records[0].Rev)
	require.NotEmpty(t, output.Cursor)
	require.Equal(t, output.Records[1].Rev, output.Cursor)
}

func TestServer_GetRepoOplog_Since(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"x": 1})
	require.NoError(t, err)
	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k2", map[string]any{"x": 2})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepoOplog?space="+uri.String()+"&repo=did:plc:owner",
		nil,
	)
	w := httptest.NewRecorder()
	s.GetRepoOplog(w, req)
	var first habitat.NetworkHabitatSpaceGetRepoOplogOutput
	err = json.NewDecoder(w.Body).Decode(&first)
	require.NoError(t, err)
	require.Len(t, first.Records, 2)

	req = httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepoOplog?space="+uri.String()+"&repo=did:plc:owner&since="+first.Cursor,
		nil,
	)
	w = httptest.NewRecorder()
	s.GetRepoOplog(w, req)
	var second habitat.NetworkHabitatSpaceGetRepoOplogOutput
	err = json.NewDecoder(w.Body).Decode(&second)
	require.NoError(t, err)
	require.Len(t, second.Records, 0)
}

func TestServer_GetRepoOplog_Unauthorized(t *testing.T) {
	s := newAliceServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepoOplog?space="+uri.String()+"&repo=did:plc:owner",
		nil,
	)
	w := httptest.NewRecorder()
	s.GetRepoOplog(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestServer_GetRepoOplog_IncludesValue(t *testing.T) {
	s := newOwnerServer(t)

	uri, err := s.store.CreateSpace(t.Context(), owner, groupType, "test")
	require.NoError(t, err)

	coll := syntax.NSID("network.habitat.note")

	err = s.store.PutRecord(t.Context(), uri, owner, coll, "k1", map[string]any{"text": "hello"})
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet,
		"/xrpc/network.habitat.space.getRepoOplog?space="+uri.String()+"&repo=did:plc:owner",
		nil,
	)
	w := httptest.NewRecorder()
	s.GetRepoOplog(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var output habitat.NetworkHabitatSpaceGetRepoOplogOutput
	err = json.NewDecoder(w.Body).Decode(&output)
	require.NoError(t, err)
	require.Len(t, output.Records, 1)
	val, ok := output.Records[0].Value.(map[string]any)
	require.True(t, ok)
	require.Equal(t, "hello", val["text"])
}
