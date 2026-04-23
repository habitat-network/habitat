package hive

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMintThenLookup(t *testing.T) {
	h := newTestHive(t, "example.com", "")
	s, err := NewServer(h)
	require.NoError(t, err)

	// Mint via HTTP handler
	body, _ := json.Marshal(map[string]string{"handle": "alice"})
	mintReq := httptest.NewRequest(http.MethodPost, "/xrpc/network.habitat.hive.mintIdentity", bytes.NewReader(body))
	mintReq.Header.Set("Content-Type", "application/json")
	mintW := httptest.NewRecorder()
	s.MintIdentity(mintW, mintReq)
	require.Equal(t, http.StatusOK, mintW.Code)

	// Resolve handle -> DID via HTTP handler
	handleReq := httptest.NewRequest(http.MethodGet, "/.well-known/atproto-did", nil)
	handleReq.Host = "alice.example.com"
	handleW := httptest.NewRecorder()
	s.ServeHandle(handleW, handleReq)
	require.Equal(t, http.StatusOK, handleW.Code)
	did := strings.TrimSpace(handleW.Body.String())
	require.True(t, strings.HasPrefix(did, "did:web:"))

	// Resolve DID -> DID doc via HTTP handler
	didHost := strings.TrimPrefix(did, "did:web:")
	docReq := httptest.NewRequest(http.MethodGet, "/.well-known/did.json", nil)
	docReq.Host = didHost
	docW := httptest.NewRecorder()
	s.ServeDIDDoc(docW, docReq)
	require.Equal(t, http.StatusOK, docW.Code)
	require.Equal(t, "application/did+ld+json", docW.Header().Get("Content-Type"))

	var doc map[string]any
	require.NoError(t, json.Unmarshal(docW.Body.Bytes(), &doc))
	require.Equal(t, did, doc["id"])
	aka := doc["alsoKnownAs"]
	akaSlice, ok := aka.([]interface{})
	require.True(t, ok)
	require.Len(t, akaSlice, 1)
	require.Equal(t, "at://alice.example.com", akaSlice[0])
}
