package authzen

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/clique"
	"github.com/habitat-network/habitat/internal/inbox"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/pear"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
)

const (
	ownerDID   = syntax.DID("did:plc:bob")
	aliceDID   = syntax.DID("did:plc:alice")
	charlieDID = syntax.DID("did:plc:charlie")
	collection = syntax.NSID("network.habitat.photo")
	rkey       = syntax.RecordKey("abc123")
)

func newTestPear(t *testing.T) pear.Pear {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)

	cs, err := clique.NewStore(db)
	require.NoError(t, err)

	ps, err := permissions.NewStore(db, cs)
	require.NoError(t, err)

	cdc := repo.NewChangeEmitter(t.Context(), repo.DefaultChangeBufferSize)
	r, err := repo.NewRepo(cdc, db)
	require.NoError(t, err)

	ib, err := inbox.New(db)
	require.NoError(t, err)

	n := node.NewDummy()
	dir := identity.DefaultDirectory()

	return pear.NewPear(n, dir, ps, r, ib)
}

func seedPermission(t *testing.T, p pear.Pear, grantee syntax.DID) {
	t.Helper()
	err := p.AddPermissions(t.Context(), ownerDID, []permissions.Grantee{permissions.DIDGrantee(grantee)}, ownerDID, collection, rkey)
	require.NoError(t, err)
}

func newTestServer(t *testing.T, p pear.Pear) *Server {
	t.Helper()
	return NewServer(p, authn.NewStubAuthnForTest(ownerDID), authn.NewStubAuthnForTest(ownerDID))
}

func jsonBody(t *testing.T, v any) *strings.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return strings.NewReader(string(b))
}

func requireDecision(t *testing.T, resp *http.Response, expected bool) {
	t.Helper()
	defer resp.Body.Close()
	var actual habitat.NetworkHabitatAuthzenEvaluateOutput
	err := json.NewDecoder(resp.Body).Decode(&actual)
	require.NoError(t, err)
	require.Equal(t, expected, actual.Decision)
}

func TestEvaluateAccess_HappyPath(t *testing.T) {
	p := newTestPear(t)
	seedPermission(t, p, aliceDID)
	s := newTestServer(t, p)

	body := jsonBody(t, map[string]any{
		"subject":  map[string]any{"type": "user", "id": aliceDID.String()},
		"resource": map[string]any{"type": "habitat.record", "id": "habitat://did:plc:bob/network.habitat.photo/abc123"},
		"action":   map[string]any{"name": "can_read"},
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.authzen.evaluate", body)
	resp := httptest.NewRecorder()
	s.EvaluateAccess(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	requireDecision(t, resp.Result(), true)
}

func TestEvaluateAccess_Denied(t *testing.T) {
	p := newTestPear(t)
	seedPermission(t, p, aliceDID) // only alice is granted
	s := newTestServer(t, p)

	body := jsonBody(t, map[string]any{
		"subject":  map[string]any{"type": "user", "id": charlieDID.String()},
		"resource": map[string]any{"type": "habitat.record", "id": "habitat://did:plc:bob/network.habitat.photo/abc123"},
		"action":   map[string]any{"name": "can_read"},
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.authzen.evaluate", body)
	resp := httptest.NewRecorder()
	s.EvaluateAccess(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	requireDecision(t, resp.Result(), false)
}

func TestEvaluateAccess_UnknownAction(t *testing.T) {
	p := newTestPear(t)
	seedPermission(t, p, aliceDID)
	s := newTestServer(t, p)

	body := jsonBody(t, map[string]any{
		"subject":  map[string]any{"type": "user", "id": aliceDID.String()},
		"resource": map[string]any{"type": "habitat.record", "id": "habitat://did:plc:bob/network.habitat.photo/abc123"},
		"action":   map[string]any{"name": "can_write"},
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.authzen.evaluate", body)
	resp := httptest.NewRecorder()
	s.EvaluateAccess(resp, req)

	require.Equal(t, http.StatusOK, resp.Code)
	requireDecision(t, resp.Result(), false)
}

func TestEvaluateAccess_InvalidSubjectID(t *testing.T) {
	p := newTestPear(t)
	s := newTestServer(t, p)

	body := jsonBody(t, map[string]any{
		"subject":  map[string]any{"type": "user", "id": "not-a-did"},
		"resource": map[string]any{"type": "habitat.record", "id": "habitat://did:plc:bob/network.habitat.photo/abc123"},
		"action":   map[string]any{"name": "can_read"},
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.authzen.evaluate", body)
	resp := httptest.NewRecorder()
	s.EvaluateAccess(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestEvaluateAccess_InvalidResourceID(t *testing.T) {
	p := newTestPear(t)
	s := newTestServer(t, p)

	body := jsonBody(t, map[string]any{
		"subject":  map[string]any{"type": "user", "id": aliceDID.String()},
		"resource": map[string]any{"type": "habitat.record", "id": "not-a-uri"},
		"action":   map[string]any{"name": "can_read"},
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.authzen.evaluate", body)
	resp := httptest.NewRecorder()
	s.EvaluateAccess(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}

func TestEvaluateAccess_MissingSubject(t *testing.T) {
	p := newTestPear(t)
	s := newTestServer(t, p)

	body := jsonBody(t, map[string]any{
		"resource": map[string]any{"type": "habitat.record", "id": "habitat://did:plc:bob/network.habitat.photo/abc123"},
		"action":   map[string]any{"name": "can_read"},
	})
	req := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.authzen.evaluate", body)
	resp := httptest.NewRecorder()
	s.EvaluateAccess(resp, req)

	require.Equal(t, http.StatusBadRequest, resp.Code)
}
