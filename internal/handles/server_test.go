package handles

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var testDID = syntax.DID("did:web:abc123.example.com")

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := New(db)
	require.NoError(t, err)
	srv, err := NewServer(h)
	require.NoError(t, err)
	return srv
}

func TestServeHandle(t *testing.T) {
	srv := newTestServer(t)
	require.NoError(t, srv.handles.MintHandle(t.Context(), "alice.example.com", testDID))

	r := httptest.NewRequest(http.MethodGet, "/.well-known/atproto-did", nil)
	r.Host = "alice.example.com"
	w := httptest.NewRecorder()
	srv.ServeHandle(w, r)
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "text/plain", w.Header().Get("Content-Type"))
	require.Equal(t, "did:web:abc123.example.com", w.Body.String())
}

func TestServeHandle_NotFound(t *testing.T) {
	srv := newTestServer(t)

	r := httptest.NewRequest(http.MethodGet, "/.well-known/atproto-did", nil)
	r.Host = "nobody.example.com"
	w := httptest.NewRecorder()
	srv.ServeHandle(w, r)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestServeMintHandle(t *testing.T) {
	srv := newTestServer(t)

	body, _ := json.Marshal(map[string]string{
		"handle": "alice.example.com",
		"did":    string(testDID),
	})
	r := httptest.NewRequest(http.MethodPost, "/mint-handle", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.MintHandle(w, r)
	require.Equal(t, http.StatusCreated, w.Code)

	handle, err := syntax.ParseHandle("alice.example.com")
	require.NoError(t, err)
	ident, err := srv.handles.LookupHandle(t.Context(), handle)
	require.NoError(t, err)
	require.Equal(t, testDID, ident.DID)
}

func TestServeMintHandle_WrongCaller(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := New(db)
	require.NoError(t, err)
	srv, err := NewServer(h)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{
		"handle": "alice.example.com",
		"did":    string(testDID),
	})
	r := httptest.NewRequest(http.MethodPost, "/mint-handle", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.MintHandle(w, r)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServeMintHandle_InvalidBody(t *testing.T) {
	srv := newTestServer(t)

	r := httptest.NewRequest(http.MethodPost, "/mint-handle", bytes.NewReader([]byte("invalid")))
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	srv.MintHandle(w, r)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeMintHandle_WrongMethod(t *testing.T) {
	srv := newTestServer(t)

	r := httptest.NewRequest(http.MethodGet, "/mint-handle", nil)
	w := httptest.NewRecorder()
	srv.MintHandle(w, r)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
