package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthClient_ExchangeCode_Success(t *testing.T) {
	server := fakeTokenServer(t, map[string]interface{}{
		"token": TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
			Scope:        "atproto transition:generic",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		},
	})
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: server.URL + "/token",
	}

	tokenResp, err := client.ExchangeCode(dpopClient, "test-code", "https://example.com", state)

	require.NoError(t, err)
	require.NotNil(t, tokenResp)
	require.Equal(t, "test-access-token", tokenResp.AccessToken)
	require.Equal(t, "test-refresh-token", tokenResp.RefreshToken)
	require.Equal(t, "atproto transition:generic", tokenResp.Scope)
	require.Equal(t, "Bearer", tokenResp.TokenType)
	require.Equal(t, 3600, tokenResp.ExpiresIn)
}

func TestOAuthClient_ExchangeCode_ClientAssertionError(t *testing.T) {
	// Create a client with invalid JWK to trigger client assertion error
	invalidJwk := []byte(`{"invalid": "jwk"}`)
	_, err := NewOAuthClient("test-client", "https://test.com", "https://test.com/callback", invalidJwk)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown json web key type")
}

func TestOAuthClient_ExchangeCode_HTTPRequestError(t *testing.T) {
	// Create a server that doesn't respond to simulate network error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't respond, just close the connection
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: server.URL + "/token",
	}

	_, err := client.ExchangeCode(dpopClient, "test-code", "https://example.com", state)

	require.Error(t, err)
	require.Contains(t, err.Error(), "EOF")
}

func TestOAuthClient_ExchangeCode_NonOKStatus(t *testing.T) {
	server := fakeTokenServer(t, map[string]interface{}{
		"token-status": http.StatusBadRequest,
		"token-error":  "invalid_grant",
	})
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: server.URL + "/token",
	}

	_, err := client.ExchangeCode(dpopClient, "test-code", "https://example.com", state)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to exchange code")
	require.Contains(t, err.Error(), "400 Bad Request")
}

func TestOAuthClient_ExchangeCode_RequestParameters(t *testing.T) {
	var capturedRequest *http.Request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedRequest = r

		// Parse form to check parameters
		err := r.ParseForm()
		if err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}

		// Verify required parameters are present
		requiredParams := []string{"client_id", "grant_type", "redirect_uri", "code", "code_verifier", "client_assertion_type", "client_assertion"}
		for _, param := range requiredParams {
			if r.FormValue(param) == "" {
				http.Error(w, "missing parameter: "+param, http.StatusBadRequest)
				return
			}
		}

		// Verify parameter values
		if r.FormValue("client_id") != "test-client" {
			http.Error(w, "invalid client_id", http.StatusBadRequest)
			return
		}
		if r.FormValue("grant_type") != "authorization_code" {
			http.Error(w, "invalid grant_type", http.StatusBadRequest)
			return
		}
		if r.FormValue("redirect_uri") != "https://test.com/callback" {
			http.Error(w, "invalid redirect_uri", http.StatusBadRequest)
			return
		}
		if r.FormValue("code") != "test-code" {
			http.Error(w, "invalid code", http.StatusBadRequest)
			return
		}
		if r.FormValue("code_verifier") != "test-verifier" {
			http.Error(w, "invalid code_verifier", http.StatusBadRequest)
			return
		}
		if r.FormValue("client_assertion_type") != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
			http.Error(w, "invalid client_assertion_type", http.StatusBadRequest)
			return
		}
		if r.FormValue("client_assertion") == "" {
			http.Error(w, "missing client_assertion", http.StatusBadRequest)
			return
		}

		// Return success response
		err = json.NewEncoder(w).Encode(TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
			Scope:        "atproto transition:generic",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		})
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
	}))
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: server.URL + "/token",
	}

	tokenResp, err := client.ExchangeCode(dpopClient, "test-code", "https://example.com", state)

	require.NoError(t, err)
	require.NotNil(t, tokenResp)
	require.NotNil(t, capturedRequest)
	require.Equal(t, "application/x-www-form-urlencoded", capturedRequest.Header.Get("Content-Type"))
	require.Equal(t, "POST", capturedRequest.Method)
}
