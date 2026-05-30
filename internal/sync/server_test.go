package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/authn"
)

// mockStore implements SpaceStore for testing sync server handlers.
type mockStore struct {
	listSpacesFn        func(ctx context.Context, member syntax.DID, filterOwner *syntax.DID, filterType *syntax.NSID) ([]SpaceView, error)
	getSpaceStateFn     func(ctx context.Context, space string) (*SpaceState, error)
	listRecordChangesFn func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error)
	getMemberOplogFn    func(ctx context.Context, space string, since string, limit int) ([]MemberOp, error)
	isMemberFn          func(ctx context.Context, space string, did string) (bool, error)
	getSpaceFn          func(ctx context.Context, space string) (*SpaceView, error)
}

func (m *mockStore) ListSpaces(ctx context.Context, member syntax.DID, filterOwner *syntax.DID, filterType *syntax.NSID) ([]SpaceView, error) {
	return m.listSpacesFn(ctx, member, filterOwner, filterType)
}
func (m *mockStore) GetSpaceState(ctx context.Context, space string) (*SpaceState, error) {
	return m.getSpaceStateFn(ctx, space)
}
func (m *mockStore) ListRecordChanges(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
	return m.listRecordChangesFn(ctx, space, repo, since, limit)
}
func (m *mockStore) GetMemberOplog(ctx context.Context, space string, since string, limit int) ([]MemberOp, error) {
	return m.getMemberOplogFn(ctx, space, since, limit)
}
func (m *mockStore) IsMember(ctx context.Context, space string, did string) (bool, error) {
	return m.isMemberFn(ctx, space, did)
}
func (m *mockStore) GetSpace(ctx context.Context, space string) (*SpaceView, error) {
	return m.getSpaceFn(ctx, space)
}

func authOK() authn.Method {
	return authn.NewStubAuthnForTest(syntax.DID("did:plc:test"))
}

func authFail() authn.Method {
	return authn.NewStubAuthnFailedForTest()
}

func newTestSyncServer(store SpaceStore, oauth authn.Method) *Server {
	f := NewFanout()
	return NewServer(store, f, oauth)
}

// Auth failure tests

func TestHandleListSpaces_Unauthorized(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authFail())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listSpaces", nil)
	w := httptest.NewRecorder()
	s.HandleListSpaces(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleGetSpaceState_Unauthorized(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authFail())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getSpaceState?space=ats://did:plc:test/network.habitat.group/test", nil)
	w := httptest.NewRecorder()
	s.HandleGetSpaceState(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleListRecords_Unauthorized(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authFail())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/network.habitat.group/test", nil)
	w := httptest.NewRecorder()
	s.HandleListRecords(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleListRecordChanges_Unauthorized(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authFail())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecordChanges?space=ats://did:plc:test/network.habitat.group/test", nil)
	w := httptest.NewRecorder()
	s.HandleListRecordChanges(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHandleGetMemberOplog_Unauthorized(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authFail())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getMemberOplog?space=ats://did:plc:test/network.habitat.group/test", nil)
	w := httptest.NewRecorder()
	s.HandleGetMemberOplog(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

// Missing required param tests

func TestHandleListRecords_MissingSpace(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecords(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	require.Equal(t, "InvalidRequest", body["error"])
}

func TestHandleListRecordChanges_MissingSpace(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecordChanges", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecordChanges(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetMemberOplog_MissingSpace(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getMemberOplog", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetMemberOplog(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleGetSpaceState_MissingSpace(t *testing.T) {
	mock := &mockStore{}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getSpaceState", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetSpaceState(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// Invalid param format test

func TestHandleListSpaces_InvalidSpaceTypes(t *testing.T) {
	mock := &mockStore{
		listSpacesFn: func(ctx context.Context, member syntax.DID, filterOwner *syntax.DID, filterType *syntax.NSID) ([]SpaceView, error) {
			return []SpaceView{}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listSpaces?spaceTypes=not-a-valid-nsid", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListSpaces(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

// Store error propagation tests

func TestHandleListSpaces_StoreError(t *testing.T) {
	mock := &mockStore{
		listSpacesFn: func(ctx context.Context, member syntax.DID, filterOwner *syntax.DID, filterType *syntax.NSID) ([]SpaceView, error) {
			return nil, fmt.Errorf("store error")
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listSpaces", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListSpaces(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetSpaceState_StoreError(t *testing.T) {
	mock := &mockStore{
		getSpaceStateFn: func(ctx context.Context, space string) (*SpaceState, error) {
			return nil, fmt.Errorf("store error")
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getSpaceState?space=ats://did:plc:test/network.habitat.group/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetSpaceState(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetSpaceState_NotFound(t *testing.T) {
	mock := &mockStore{
		getSpaceStateFn: func(ctx context.Context, space string) (*SpaceState, error) {
			return nil, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getSpaceState?space=ats://did:plc:test/network.habitat.group/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetSpaceState(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleListRecords_StoreError(t *testing.T) {
	mock := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			return nil, fmt.Errorf("store error")
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/network.habitat.group/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecords(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleListRecordChanges_StoreError(t *testing.T) {
	mock := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			return nil, fmt.Errorf("store error")
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecordChanges?space=ats://did:plc:test/network.habitat.group/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecordChanges(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestHandleGetMemberOplog_StoreError(t *testing.T) {
	mock := &mockStore{
		getMemberOplogFn: func(ctx context.Context, space string, since string, limit int) ([]MemberOp, error) {
			return nil, fmt.Errorf("store error")
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getMemberOplog?space=ats://did:plc:test/network.habitat.group/test", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetMemberOplog(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code)
}

// Limit parsing tests

func TestHandleListRecords_InvalidLimit(t *testing.T) {
	// invalid string -> defaults to 50
	invalidCalled := false
	mock := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			invalidCalled = true
			assert.Equal(t, 50, limit)
			return []RecordChange{}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())
	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/network.habitat.group/test&limit=invalid", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecords(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, invalidCalled)

	// negative -> defaults to 50
	negCalled := false
	mock2 := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			negCalled = true
			assert.Equal(t, 50, limit)
			return []RecordChange{}, nil
		},
	}
	s2 := newTestSyncServer(mock2, authOK())
	req2 := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/network.habitat.group/test&limit=-5", nil)
	req2.Header.Set("Authorization", "Bearer test-token")
	w2 := httptest.NewRecorder()
	s2.HandleListRecords(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	require.True(t, negCalled)

	// over max -> defaults to 50 (code only accepts 1-100)
	capCalled := false
	mock3 := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			capCalled = true
			assert.Equal(t, 50, limit)
			return []RecordChange{}, nil
		},
	}
	s3 := newTestSyncServer(mock3, authOK())
	req3 := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/network.habitat.group/test&limit=999", nil)
	req3.Header.Set("Authorization", "Bearer test-token")
	w3 := httptest.NewRecorder()
	s3.HandleListRecords(w3, req3)
	require.Equal(t, http.StatusOK, w3.Code)
	require.True(t, capCalled)
}

// Happy path response shape tests

func TestHandleListSpaces_Success(t *testing.T) {
	mock := &mockStore{
		listSpacesFn: func(ctx context.Context, member syntax.DID, filterOwner *syntax.DID, filterType *syntax.NSID) ([]SpaceView, error) {
			return []SpaceView{
				{Space: "ats://did:plc:test/net.example.app/space1", Type: "net.example.app", SpaceRev: "3jkl"},
				{Space: "ats://did:plc:test/net.example.app/space2", Type: "net.example.app", SpaceRev: "3jkm"},
			}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listSpaces", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListSpaces(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)

	spaces, ok := body["spaces"].([]interface{})
	require.True(t, ok)
	require.Len(t, spaces, 2)
}

func TestHandleGetSpaceState_Success(t *testing.T) {
	mock := &mockStore{
		getSpaceStateFn: func(ctx context.Context, space string) (*SpaceState, error) {
			return &SpaceState{
				Space:     space,
				SpaceType: "net.example.app",
				SpaceRev:  "3jkl",
				MemberRev: "3jkm",
				Repos: []SpaceRepoState{
					{DID: "did:plc:alice", Rev: "3jkn"},
				},
			}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getSpaceState?space=ats://did:plc:test/net.example.app/space1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetSpaceState(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)

	require.Equal(t, "ats://did:plc:test/net.example.app/space1", body["space"])
	require.Equal(t, "3jkl", body["spaceRev"])
	repos := body["repos"].([]interface{})
	require.Len(t, repos, 1)
}

func TestHandleListRecords_Success(t *testing.T) {
	mock := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			val := map[string]any{"text": "hello"}
			return []RecordChange{
				{Space: space, Repo: "did:plc:alice", Rev: "3jkl", Action: "upsert", Collection: "net.example.note", Rkey: "r1", Value: &val},
			}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/net.example.app/space1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecords(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)

	records := body["records"].([]interface{})
	require.Len(t, records, 1)
	require.NotEmpty(t, body["cursor"])
}

func TestHandleListRecords_ExcludesDeletes(t *testing.T) {
	mock := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			val := map[string]any{"text": "hello"}
			return []RecordChange{
				{Space: space, Repo: "did:plc:alice", Rev: "3jkl", Action: "upsert", Collection: "net.example.note", Rkey: "r1", Value: &val},
				{Space: space, Repo: "did:plc:alice", Rev: "3jkm", Action: "delete", Collection: "net.example.note", Rkey: "r2", Value: nil},
			}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecords?space=ats://did:plc:test/net.example.app/space1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecords(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)

	records := body["records"].([]interface{})
	require.Len(t, records, 1, "deleted records should be excluded from listRecords")
}

func TestHandleListRecordChanges_Success(t *testing.T) {
	mock := &mockStore{
		listRecordChangesFn: func(ctx context.Context, space string, repo string, since string, limit int) ([]RecordChange, error) {
			val := map[string]any{"x": 1}
			return []RecordChange{
				{Space: space, Rev: "3jkl", Action: "upsert", Collection: "net.example.note", Rkey: "r1", Value: &val},
			}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.listRecordChanges?space=ats://did:plc:test/net.example.app/space1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleListRecordChanges(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)

	changes := body["changes"].([]interface{})
	require.Len(t, changes, 1)
	require.NotEmpty(t, body["cursor"])
}

func TestHandleGetMemberOplog_Success(t *testing.T) {
	access := "read"
	mock := &mockStore{
		getMemberOplogFn: func(ctx context.Context, space string, since string, limit int) ([]MemberOp, error) {
			return []MemberOp{
				{Space: space, Rev: "3jkl", Idx: 0, Action: "add", DID: "did:plc:alice", Access: &access},
			}, nil
		},
	}
	s := newTestSyncServer(mock, authOK())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/network.habitat.sync.getMemberOplog?space=ats://did:plc:test/net.example.app/space1", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	w := httptest.NewRecorder()
	s.HandleGetMemberOplog(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]interface{}
	err := json.NewDecoder(w.Body).Decode(&body)
	require.NoError(t, err)

	ops := body["ops"].([]interface{})
	require.Len(t, ops, 1)
	require.NotEmpty(t, body["cursor"])
}
