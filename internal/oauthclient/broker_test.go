package oauthclient

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/require"
)

// setupTestXrpcBrokerHandler creates a xrpcBrokerHandler with test dependencies
func setupTestXrpcBrokerHandler(oauthClient OAuthClient, htuURL string) *xrpcBrokerHandler {
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	return &xrpcBrokerHandler{
		htuURL:       htuURL,
		oauthClient:  oauthClient,
		sessionStore: sessionStore,
	}
}

// setupTestXrpcBrokerHandlerWithSessionStore creates a xrpcBrokerHandler with a specific session store
func setupTestXrpcBrokerHandlerWithSessionStore(oauthClient OAuthClient, htuURL string, sessionStore sessions.Store) *xrpcBrokerHandler {
	return &xrpcBrokerHandler{
		htuURL:       htuURL,
		oauthClient:  oauthClient,
		sessionStore: sessionStore,
	}
}

// setupTestRequestWithSession creates a test request with proper DPoP session cookies
func setupTestRequestWithSession(t *testing.T, sessionStore sessions.Store, opts DpopSessionOptions) (*http.Request, *httptest.ResponseRecorder) {
	// Create a test request and response writer for session creation
	sessionReq := httptest.NewRequest("GET", "/test", nil)
	sessionW := httptest.NewRecorder()

	// Use provided identity or create a default one
	var identity *identity.Identity
	if opts.Identity != nil {
		identity = opts.Identity
	} else {
		identity = testIdentity(opts.PdsURL)
	}

	// Create a fresh DPoP session without saving it yet
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	session, err := sessionStore.New(sessionReq, "dpop-session")
	require.NoError(t, err)

	dpopSession := &cookieSession{session: session}

	err = dpopSession.SetDpopKey(key)
	require.NoError(t, err)
	err = dpopSession.SetIdentity(identity)
	require.NoError(t, err)
	err = dpopSession.SetPDSURL(opts.PdsURL)
	require.NoError(t, err)

	// Set issuer if provided
	if opts.Issuer != nil {
		dpopSession.session.Values[cIssuerSessionKey] = *opts.Issuer
	}

	// Set access token if provided
	if opts.TokenInfo != nil {
		err = dpopSession.SetTokenInfo(opts.TokenInfo)
		require.NoError(t, err)
	}

	// Remove identity if explicitly requested
	if opts.RemoveIdentity {
		delete(dpopSession.session.Values, cIdentitySessionKey)
	}

	// Save the session only once at the end
	err = dpopSession.session.Save(sessionReq, sessionW)
	require.NoError(t, err)

	// Create the actual request that will be used by the handler
	req := httptest.NewRequest("POST", "/xrpc/com.atproto.repo.getRecord", nil)
	w := httptest.NewRecorder()

	// Add DPoP session cookies to the actual request
	cookies := sessionW.Result().Cookies()
	for _, cookie := range cookies {
		fmt.Println("Cookie", cookie)
		req.AddCookie(cookie)
	}

	return req, w
}

func TestXrpcBrokerHandler_SuccessfulRequest(t *testing.T) {
	// This test verifies that the XRPC broker handler properly handles requests
	// with a valid DPoP session and access token

	// Setup mock PDS server that responds to XRPC requests
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xrpc/com.atproto.repo.getRecord" {
			// Return a mock record response
			resp := map[string]interface{}{
				"cid":   "bafyreidf6z3ac6n6f2n5bs2by2eqm6j7mv3gduq7q",
				"uri":   "at://did:plc:test123/app.bsky.feed.post/3juxg",
				"value": map[string]interface{}{"text": "Hello world"},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockPDS.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))

	handler := &xrpcBrokerHandler{
		htuURL:       "https://test.com",
		oauthClient:  oauthClient,
		sessionStore: sessionStore,
	}

	// Create request with valid DPoP session
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
		},
	})

	// Set the correct path for the request
	req.URL.Path = "/xrpc/com.atproto.repo.getRecord"

	handler.ServeHTTP(w, req)

	// Should succeed with valid session
	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "bafyreidf6z3ac6n6f2n5bs2by2eqm6j7mv3gduq7q", response["cid"])
}

func TestXrpcBrokerHandler_NoDPOPSession(t *testing.T) {
	oauthClient := testOAuthClient(t)
	handler := setupTestXrpcBrokerHandler(oauthClient, "https://test.com")

	req := httptest.NewRequest("POST", "/xrpc/com.atproto.repo.getRecord", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestXrpcBrokerHandler_UnauthorizedResponse(t *testing.T) {
	// This test verifies that the XRPC broker handler properly handles unauthorized responses
	// from the PDS (the hardcoded hostname issue has been fixed)

	// Setup mock PDS server that returns unauthorized
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer mockPDS.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestXrpcBrokerHandlerWithSessionStore(oauthClient, "https://test.com", sessionStore)

	// Create request with valid DPoP session
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "expired-access-token",
			RefreshToken: "test-refresh-token",
		},
	})

	// Set the correct path for the request
	req.URL.Path = "/xrpc/com.atproto.repo.getRecord"

	handler.ServeHTTP(w, req)

	// Should fail due to token refresh error (no OAuth server configured)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "failed to fetch authorization server")
}

func TestXrpcBrokerHandler_RefreshTokenError(t *testing.T) {
	// Setup mock PDS server that returns unauthorized
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer mockPDS.Close()

	// Setup fake OAuth server that returns error for refresh
	fakeOAuthServer := fakeTokenServer(map[string]interface{}{
		"token-status": http.StatusBadRequest,
		"token-error":  "invalid_grant",
	})
	defer fakeOAuthServer.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestXrpcBrokerHandlerWithSessionStore(oauthClient, "https://test.com", sessionStore)

	// Create request with DPoP session that has refresh token
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "expired-access-token",
			RefreshToken: "invalid-refresh-token",
		},
	})

	// Set the correct path for the request
	req.URL.Path = "/xrpc/com.atproto.repo.getRecord"

	handler.ServeHTTP(w, req)

	// Should fail due to OAuth server error (no server configured)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	require.Contains(t, w.Body.String(), "failed to fetch authorization server")
}

func TestXrpcBrokerHandler_SuccessfulTokenRefresh(t *testing.T) {
	// This test verifies that the XRPC broker handler can successfully refresh tokens
	// when the PDS returns unauthorized

	// Setup fake OAuth server that returns successful token refresh
	fakeOAuthServer := fakeAuthServer(map[string]interface{}{
		"token": TokenResponse{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			Scope:        "atproto transition:generic",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		},
	})
	defer fakeOAuthServer.Close()

	// Setup mock PDS server that returns unauthorized first, then success
	// Also serves OAuth discovery endpoints
	requestCount := 0
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++

		// Serve OAuth discovery endpoints on the PDS server
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			if err := json.NewEncoder(w).Encode(oauthProtectedResource{
				AuthorizationServers: []string{fakeOAuthServer.URL + "/.well-known/oauth-authorization-server"},
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		case "/.well-known/oauth-authorization-server":
			if err := json.NewEncoder(w).Encode(oauthAuthorizationServer{
				Issuer:        "https://example.com",
				TokenEndpoint: fakeOAuthServer.URL + "/token",
				PAREndpoint:   fakeOAuthServer.URL + "/par",
				AuthEndpoint:  fakeOAuthServer.URL + "/auth",
			}); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}

		if requestCount == 1 {
			// First request returns unauthorized
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		// Second request (after token refresh) returns success
		if r.URL.Path == "/xrpc/com.atproto.repo.getRecord" {
			resp := map[string]interface{}{
				"cid":   "bafyreidf6z3ac6n6f2n5bs2by2eqm6j7mv3gduq7q",
				"uri":   "at://did:plc:test123/app.bsky.feed.post/3juxg",
				"value": map[string]interface{}{"text": "Hello world"},
			}
			if err := json.NewEncoder(w).Encode(resp); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockPDS.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestXrpcBrokerHandlerWithSessionStore(oauthClient, "https://test.com", sessionStore)

	// Create request with DPoP session that has refresh token
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "expired-access-token",
			RefreshToken: "valid-refresh-token",
		},
	})

	// Set the correct path for the request
	req.URL.Path = "/xrpc/com.atproto.repo.getRecord"

	handler.ServeHTTP(w, req)

	// Should succeed with token refresh
	require.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)
	require.Equal(t, "bafyreidf6z3ac6n6f2n5bs2by2eqm6j7mv3gduq7q", response["cid"])
}

func TestXrpcBrokerHandler_RequestBodyHandling(t *testing.T) {
	// This test verifies that the XRPC broker handler properly handles request bodies
	// with a valid DPoP session

	// Setup mock PDS server that responds to XRPC requests
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xrpc/com.atproto.repo.putRecord" {
			// Read and echo back the request body
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(body); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockPDS.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestXrpcBrokerHandlerWithSessionStore(oauthClient, "https://test.com", sessionStore)

	// Create request with body and valid DPoP session
	requestBody := `{"collection": "app.bsky.feed.post", "repo": "did:plc:test123", "rkey": "3juxg", "value": {"text": "Hello world"}}`
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
		},
	})

	// Set the correct path and body for the request
	req.URL.Path = "/xrpc/com.atproto.repo.putRecord"
	req.Body = io.NopCloser(bytes.NewBufferString(requestBody))
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(w, req)

	// Should succeed and return the request body
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, requestBody, w.Body.String())

}

func TestXrpcBrokerHandler_ErrorResponse(t *testing.T) {
	// This test verifies that the XRPC broker handler properly handles error responses
	// from the PDS with a valid DPoP session

	// Setup mock PDS server that returns an error
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xrpc/com.atproto.repo.getRecord" {
			http.Error(w, "record not found", http.StatusNotFound)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockPDS.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestXrpcBrokerHandlerWithSessionStore(oauthClient, "https://test.com", sessionStore)

	// Create request with valid DPoP session
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
		},
	})

	// Set the correct path for the request
	req.URL.Path = "/xrpc/com.atproto.repo.getRecord"

	handler.ServeHTTP(w, req)

	// Should return the error from PDS
	require.Equal(t, http.StatusNotFound, w.Code)
	require.Contains(t, w.Body.String(), "record not found")
}

func TestXrpcBrokerHandler_RequestBodyCopying(t *testing.T) {
	// This test verifies that the XRPC broker handler properly handles large request bodies
	// with a valid DPoP session

	// Setup mock PDS server that echoes back the request body
	mockPDS := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/xrpc/com.atproto.repo.putRecord" {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if _, err := w.Write(body); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer mockPDS.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestXrpcBrokerHandlerWithSessionStore(oauthClient, "https://test.com", sessionStore)

	// Create request with large body and valid DPoP session
	largeBody := bytes.Repeat([]byte("test"), 1000)
	req, w := setupTestRequestWithSession(t, sessionStore, DpopSessionOptions{
		PdsURL: mockPDS.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
		},
	})

	// Set the correct path and body for the request
	req.URL.Path = "/xrpc/com.atproto.repo.putRecord"
	req.Body = io.NopCloser(bytes.NewBuffer(largeBody))
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(w, req)

	// Should succeed and return the large body
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, string(largeBody), w.Body.String())
}
