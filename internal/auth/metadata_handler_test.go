package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetadataHandler_SuccessfulMetadata(t *testing.T) {
	oauthClient := testOAuthClient(t)
	handler := &metadataHandler{
		oauthClient: oauthClient,
	}

	req := httptest.NewRequest("GET", "/client-metadata.json", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))

	// Verify the response is valid JSON and contains expected fields
	var metadata ClientMetadata
	err := json.Unmarshal(w.Body.Bytes(), &metadata)
	require.NoError(t, err)
	require.NotEmpty(t, metadata.ClientId)
	require.NotEmpty(t, metadata.ClientUri)
	require.NotEmpty(t, metadata.RedirectUris)

	require.Equal(t, "GET", handler.Method())
	require.Equal(t, "/client-metadata.json", handler.Pattern())
	require.Equal(t, "application/json", w.Header().Get("Content-Type"))
}
