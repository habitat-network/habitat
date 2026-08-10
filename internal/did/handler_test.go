package did

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
)

func TestHandler(t *testing.T) {
	doc := New(syntax.DID("did:web:alice.example.com")).
		AtprotoKey("zkey").
		Build()
	rec := httptest.NewRecorder()
	NewHandler(doc).ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/.well-known/did.json", http.NoBody),
	)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	require.Equal(t, "max-age=3600", rec.Header().Get("Cache-Control"))

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, []any{
		"https://www.w3.org/ns/did/v1",
		"https://w3id.org/security/multikey/v1",
		"https://w3id.org/security/suites/secp256k1-2019/v1",
	}, body["@context"])
	require.Equal(t, "did:web:alice.example.com", body["id"])
	require.Equal(t, []any{
		map[string]any{
			"id":                 "did:web:alice.example.com#atproto",
			"type":               "Multikey",
			"controller":         "did:web:alice.example.com",
			"publicKeyMultibase": "zkey",
		},
	}, body["verificationMethod"])
}
