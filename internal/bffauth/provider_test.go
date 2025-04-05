package bffauth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/crypto"
	"github.com/stretchr/testify/require"
)

func TestBffProvider(t *testing.T) {
	persister := NewInMemorySessionPersister()
	signingKey := []byte("test-signing-key")
	p := NewProvider(persister, signingKey)

	// The caller of the provider (i.e. another PDS) has its own set of keys
	privateKey, err := crypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	publicKey, err := privateKey.PublicKey()
	require.NoError(t, err)

	// The caller requests a challenge
	b, err := json.Marshal(ChallengeRequest{
		DID:                "my-did",
		PublicKeyMultibase: publicKey.Bytes(),
	})
	require.NoError(t, err)
	rec := httptest.NewRecorder()
	handler := http.HandlerFunc(p.handleChallenge)
	handler.ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/doesntmatter", bytes.NewBuffer(b)))
	require.Equal(t, http.StatusOK, rec.Result().StatusCode)
	respBytes, err := io.ReadAll(rec.Result().Body)
	require.NoError(t, err)

	var resp ChallengeResponse
	require.NoError(t, json.Unmarshal(respBytes, &resp))

	// The caller gets a challenge and session id
	challenge := resp.Challenge
	session := resp.Session

	proof, err := GenerateProof(challenge, privateKey)
	require.NoError(t, err)

	// The caller responds with the proof
	b, err = json.Marshal(AuthRequest{
		SessionID: session,
		Proof:     proof,
	})
	require.NoError(t, err)
	rec = httptest.NewRecorder()
	handler = http.HandlerFunc(p.handleAuth)
	handler.ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodGet, "/doesntmatter", bytes.NewBuffer(b)))
	require.Equal(t, http.StatusOK, rec.Result().StatusCode)
	respBytes, err = io.ReadAll(rec.Result().Body)
	require.NoError(t, err)

	// Use the test endpoint to make sure the token works
	var authResp AuthResponse
	require.NoError(t, json.Unmarshal(respBytes, &authResp))

	req := httptest.NewRequest(http.MethodGet, "/doesntmatter", nil)
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", authResp.Token))
	rec = httptest.NewRecorder()
	handler = http.HandlerFunc(p.handleTest)
	handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Result().StatusCode)
	respBytes, err = io.ReadAll(rec.Result().Body)
	require.NoError(t, err)
	var testResp map[string]string
	err = json.Unmarshal(respBytes, &testResp)
	require.NoError(t, err)
	require.Contains(t, testResp, "message")
	require.Equal(t, testResp["message"], "Hello, world!")
}
