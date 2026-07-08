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
