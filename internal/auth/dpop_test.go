package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"
)

// TestNonceProvider is a simple in-memory nonce provider for testing
type TestNonceProvider struct {
	nonce      string
	errorOnGet bool
}

func (t *TestNonceProvider) GetDpopNonce() (string, bool, error) {
	if t.nonce == "" && !t.errorOnGet {
		return "", false, nil
	}
	if t.errorOnGet {
		return "", false, errors.New("test nonce provider error")
	}
	return t.nonce, true, nil
}

func (t *TestNonceProvider) SetDpopNonce(nonce string) error {
	t.nonce = nonce
	return nil
}

func TestDpopHttpClient_InitialValidNonce(t *testing.T) {
	// Create test server that accepts requests with valid nonce
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify DPoP header is present
		dpopHeader := r.Header.Get("DPoP")
		require.NotEmpty(t, dpopHeader, "DPoP header should be present")

		// Verify DPoP token is valid JWT
		_, err := jwt.ParseSigned(dpopHeader)
		require.NoError(t, err, "DPoP token should be valid JWT")

		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte("success"))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider with initial nonce
	nonceProvider := &TestNonceProvider{nonce: "test-nonce-123"}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider)

	// Create request
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	require.NoError(t, err)

	// Execute request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	// Verify response
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDpopHttpClient_NoNonceAtBeginning(t *testing.T) {
	// Create test server that requires nonce on first request
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if DPoP header is present and valid
		dpopHeader := r.Header.Get("DPoP")
		if dpopHeader == "" {
			// No DPoP header, return use_dpop_nonce error
			w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
			w.Header().Set("DPoP-Nonce", "new-nonce-456")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Try to parse the DPoP token to check if it has a nonce
		token, err := jwt.ParseSigned(dpopHeader)
		if err != nil {
			// Invalid token, return use_dpop_nonce error
			w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
			w.Header().Set("DPoP-Nonce", "new-nonce-456")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Check if token has nonce claim
		var claims dpopClaims
		err = token.UnsafeClaimsWithoutVerification(&claims)
		if err != nil || claims.Nonce == "" {
			// No nonce in token, return use_dpop_nonce error
			w.Header().Set("WWW-Authenticate", `DPoP error="use_dpop_nonce"`)
			w.Header().Set("DPoP-Nonce", "new-nonce-456")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		// Valid DPoP token with nonce, return success
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte("success"))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider with no initial nonce
	nonceProvider := &TestNonceProvider{}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider)

	// Create request
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	require.NoError(t, err)

	// Execute request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	// Verify response
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify nonce was set
	nonce, ok, err := nonceProvider.GetDpopNonce()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "new-nonce-456", nonce)
}

func TestDpopHttpClient_AuthorizationServerNonceError(t *testing.T) {
	// Create test server that returns use_dpop_nonce error in JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check if DPoP header is present and valid
		dpopHeader := r.Header.Get("DPoP")
		if dpopHeader == "" {
			// No DPoP header, return use_dpop_nonce error in JSON
			w.Header().Set("DPoP-Nonce", "auth-server-nonce-789")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			err := json.NewEncoder(w).Encode(map[string]string{
				"error": "use_dpop_nonce",
			})
			require.NoError(t, err)
			return
		}

		// Try to parse the DPoP token to check if it has a nonce
		token, err := jwt.ParseSigned(dpopHeader)
		if err != nil {
			// Invalid token, return use_dpop_nonce error in JSON
			w.Header().Set("DPoP-Nonce", "auth-server-nonce-789")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			err := json.NewEncoder(w).Encode(map[string]string{
				"error": "use_dpop_nonce",
			})
			require.NoError(t, err)
			return
		}

		// Check if token has nonce claim
		var claims dpopClaims
		err = token.UnsafeClaimsWithoutVerification(&claims)
		if err != nil || claims.Nonce == "" {
			// No nonce in token, return use_dpop_nonce error in JSON
			w.Header().Set("DPoP-Nonce", "auth-server-nonce-789")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			err := json.NewEncoder(w).Encode(map[string]string{
				"error": "use_dpop_nonce",
			})
			require.NoError(t, err)
			return
		}

		// Valid DPoP token with nonce, return success
		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte("success"))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider with no initial nonce
	nonceProvider := &TestNonceProvider{}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider)

	// Create request
	req, err := http.NewRequest("POST", server.URL+"/token", strings.NewReader("test-body"))
	require.NoError(t, err)

	// Execute request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	// Verify response
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// Verify nonce was set
	nonce, ok, err := nonceProvider.GetDpopNonce()
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "auth-server-nonce-789", nonce)
}

func TestDpopHttpClient_WithAccessToken(t *testing.T) {
	// Create test server that verifies access token hash
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify DPoP header is present
		dpopHeader := r.Header.Get("DPoP")
		require.NotEmpty(t, dpopHeader, "DPoP header should be present")

		// Parse and verify DPoP token
		token, err := jwt.ParseSigned(dpopHeader)
		require.NoError(t, err, "DPoP token should be valid JWT")

		// Verify claims
		var claims dpopClaims
		err = token.UnsafeClaimsWithoutVerification(&claims)
		require.NoError(t, err)

		// Verify access token hash is present
		require.NotEmpty(t, claims.AccessTokenHash, "Access token hash should be present")

		// Verify authorization header is present
		require.NotEmpty(t, r.Header.Get("Authorization"), "Authorization header should be present")
		require.Equal(
			t,
			"DPoP test-access-token",
			r.Header.Get("Authorization"),
			"Authorization header should be DPoP",
		)

		// Verify the hash matches the token
		expectedToken := "test-access-token"
		h := sha256.New()
		h.Write([]byte(expectedToken))
		expectedHash := base64.RawURLEncoding.EncodeToString(h.Sum(nil))
		require.Equal(t, expectedHash, claims.AccessTokenHash, "Access token hash should match")

		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte("success"))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider
	nonceProvider := &TestNonceProvider{nonce: "test-nonce"}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider, WithAccessToken("test-access-token"))

	// Create request with Authorization header
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	require.NoError(t, err)

	// Execute request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	// Verify response
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDpopHttpClient_RequestFormat(t *testing.T) {
	// Create test server that verifies request body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		dpopHeader := r.Header.Get("DPoP")
		require.NotEmpty(t, dpopHeader, "DPoP header should be present")

		// Verify it's a valid JWT (three parts separated by dots)
		parts := strings.Split(dpopHeader, ".")
		require.Equal(t, 3, len(parts), "DPoP token should be a valid JWT with 3 parts")

		// Read and verify request body
		bodyBytes, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		require.Equal(t, "test-request-body", string(bodyBytes), "Request body should be preserved")

		w.WriteHeader(http.StatusOK)
		_, err = w.Write([]byte("success"))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider
	nonceProvider := &TestNonceProvider{nonce: "test-nonce"}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider)

	// Create request with body
	req, err := http.NewRequest("POST", server.URL+"/test", strings.NewReader("test-request-body"))
	require.NoError(t, err)

	// Execute request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	// Verify response
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestDpopHttpClient_NonceProviderError(t *testing.T) {
	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider that returns error
	nonceProvider := &TestNonceProvider{errorOnGet: true}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider)

	// Create request
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	require.NoError(t, err)

	// Execute request - should succeed even without nonce
	resp, err := client.Do(req)
	require.Error(t, err)
	require.Nil(t, resp)
}

func TestDpopHttpClient_ErrorHandling(t *testing.T) {
	// Create test server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte("server error"))
		require.NoError(t, err)
	}))
	defer server.Close()

	// Create test key
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	// Create nonce provider
	nonceProvider := &TestNonceProvider{nonce: "test-nonce"}

	// Create DPoP client
	client := NewDpopHttpClient(key, nonceProvider)

	// Create request
	req, err := http.NewRequest("GET", server.URL+"/test", nil)
	require.NoError(t, err)

	// Execute request
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()

	// Verify response status
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}
