package httpx

import (
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
	WriteInvalidRequest(t.Context(), w, "foo", nil)
	require.JSONEq(t, `{"error":"InvalidRequest", "message":"foo"}`, w.Body.String())
}

func TestWriteSpaceNotFound(t *testing.T) {
	w := httptest.NewRecorder()
	WriteSpaceNotFound(t.Context(), w, nil)
	require.JSONEq(t, `{"error":"SpaceNotFound", "message":"foo"}`, w.Body.String())
}

func TestWriteNotSupported(t *testing.T) {
	w := httptest.NewRecorder()
	WriteNotSupported(t.Context(), w, "foo")
	require.JSONEq(t, `{"error":"NotSupported", "message":"foo"}`, w.Body.String())
}
