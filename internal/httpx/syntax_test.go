package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDIDInput(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseDIDInput(t.Context(), w, "did:web:example.com", "did")
	require.True(t, ok)
	require.Equal(t, 0, w.Body.Len())
	require.Equal(t, http.StatusOK, w.Code)
}

func TestParseDIDInput_Invalid(t *testing.T) {
	w := httptest.NewRecorder()
	_, ok := ParseDIDInput(t.Context(), w, "invalid", "did")
	require.False(t, ok)
	require.Equal(t, http.StatusBadRequest, w.Code)
	require.JSONEq(
		t,
		`{"error":"InvalidRequest", "message": "failed to parse did"}`,
		w.Body.String(),
	)
}
