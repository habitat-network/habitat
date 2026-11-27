package httpx

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWriteJSON_Success(t *testing.T) {
	w := httptest.NewRecorder()
	WriteJSON(t.Context(), w, map[string]string{"foo": "bar"})
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
	require.JSONEq(t, `{"foo":"bar"}`, w.Body.String())
	require.Equal(t, http.StatusOK, w.Code)
}

func TestWriteInvalidRequest(t *testing.T) {
	w := httptest.NewRecorder()
	WriteInvalidRequest(t.Context(), w, "foo", fmt.Errorf("bar"))
	require.JSONEq(t, `{"error":"InvalidRequest", "message":"foo"}`, w.Body.String())
}

func TestWriteSpaceNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSpaceNotFound(t.Context(), w, fmt.Errorf("foo"))
	require.JSONEq(t, `{"error":"SpaceNotFound"}`, w.Body.String())
}

func TestWriteNotSupported(t *testing.T) {
	w := httptest.NewRecorder()
	WriteNotSupported(t.Context(), w, "foo")
	require.JSONEq(t, `{"error":"NotSupported", "message":"foo"}`, w.Body.String())
}

func TestWriteRepoNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	WriteRepoNotFound(t.Context(), w, fmt.Errorf("foo"))
	require.JSONEq(t, `{"error":"RepoNotFound"}`, w.Body.String())
}

func TestWriteUnauthorized(t *testing.T) {
	w := httptest.NewRecorder()
	WriteUnauthorized(t.Context(), w, "foo", fmt.Errorf("bar"))
	require.JSONEq(t, `{"error":"Unauthorized", "message":"foo"}`, w.Body.String())
}

func TestWriteServerError(t *testing.T) {
	w := httptest.NewRecorder()
	WriteServerError(t.Context(), w, fmt.Errorf("foo"))
	require.JSONEq(t, `{"error":"ServerError", "message":"foo"}`, w.Body.String())
}

// Clients (the typed XRPC client in particular) only read an error's name and
// message from a response served as application/json, so the header has to be
// set before the status line is written.
func TestWriteError_SetsJSONContentType(t *testing.T) {
	w := httptest.NewRecorder()
	WriteInvalidRequest(t.Context(), w, "foo", fmt.Errorf("bar"))
	res := w.Result()
	defer func() { _ = res.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, res.StatusCode)
	require.Equal(t, "application/json", res.Header.Get("Content-Type"))
}
