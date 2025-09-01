package auth

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/require"
)

// setupTestCallbackHandler creates a callbackHandler with test dependencies
func setupTestCallbackHandler(t *testing.T, oauthClient OAuthClient, sessionStore sessions.Store) *callbackHandler {
	return &callbackHandler{
		oauthClient:  oauthClient,
		sessionStore: sessionStore,
	}
}

func TestCallbackHandler_SuccessfulCallback(t *testing.T) {
	// Setup fake OAuth server for token exchange
	fakeOAuthServer := fakeAuthServer(t, map[string]interface{}{
		"token": TokenResponse{
			AccessToken:  "test-access-token",
			RefreshToken: "test-refresh-token",
			Scope:        "atproto transition:generic",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		},
	})
	defer fakeOAuthServer.Close()

	// Create test dependencies
	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	// Use a single request/recorder for both session creations
	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with state first
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: fakeOAuthServer.URL + "/token",
	}
	stateJson, _ := json.Marshal(state)
	authSession.AddFlash(stateJson)
	err := authSession.Save(req, w)
	require.NoError(t, err)

	// Create DPoP session with the same request and response writer
	identity := testIdentity(fakeOAuthServer.URL)
	dpopSession, err := newCookieSession(req, sessionStore, identity, fakeOAuthServer.URL)
	require.NoError(t, err)
	err = dpopSession.SetIssuer("https://example.com")
	require.NoError(t, err)

	dpopSession.Save(req, w)

	// Add all cookies from the recorder to the request
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("code", "test-code")
	q.Add("iss", "https://example.com")
	req.URL.RawQuery = q.Encode()

	// Execute request
	handler.ServeHTTP(w, req)

	fmt.Println(w.Body.String())

	// Verify response
	require.Equal(t, http.StatusSeeOther, w.Code)
	location := w.Header().Get("Location")
	require.Equal(t, "/", location)

	// Parse Set-Cookie headers manually to get all cookies
	setCookieHeaders := w.Header().Values("Set-Cookie")
	cookieMap := make(map[string]string)

	for _, setCookieHeader := range setCookieHeaders {
		// Parse each Set-Cookie header
		cookies := parseSetCookieHeader(setCookieHeader)
		for _, cookie := range cookies {
			cookieMap[cookie.Name] = cookie.Value
		}
	}

	// The handler should set these cookies in the final response
	// Note: The cookies should be set even with the redirect
	require.NotEmpty(t, cookieMap["handle"])
	require.NotEmpty(t, cookieMap["did"])
}

// parseSetCookieHeader parses a Set-Cookie header and returns individual cookies
func parseSetCookieHeader(setCookieHeader string) []*http.Cookie {
	var cookies []*http.Cookie

	// Split by comma, but be careful about commas in cookie values
	parts := strings.Split(setCookieHeader, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		// Find the first semicolon to separate name=value from attributes
		semicolonIndex := strings.Index(part, ";")
		if semicolonIndex == -1 {
			// No attributes, just name=value
			if strings.Contains(part, "=") {
				equalIndex := strings.Index(part, "=")
				name := strings.TrimSpace(part[:equalIndex])
				value := strings.TrimSpace(part[equalIndex+1:])
				cookies = append(cookies, &http.Cookie{Name: name, Value: value})
			}
		} else {
			// Has attributes, extract name=value part
			nameValuePart := strings.TrimSpace(part[:semicolonIndex])
			if strings.Contains(nameValuePart, "=") {
				equalIndex := strings.Index(nameValuePart, "=")
				name := strings.TrimSpace(nameValuePart[:equalIndex])
				value := strings.TrimSpace(nameValuePart[equalIndex+1:])
				cookies = append(cookies, &http.Cookie{Name: name, Value: value})
			}
		}
	}

	return cookies
}

func TestCallbackHandler_NoSessionState(t *testing.T) {
	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "no state in session")
}

func TestCallbackHandler_InvalidSessionState(t *testing.T) {
	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with invalid state (not JSON)
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	authSession.AddFlash("invalid-json")
	err := authSession.Save(req, w)
	require.NoError(t, err)

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	require.Contains(t, w.Body.String(), "no state in session")
}

func TestCallbackHandler_MissingCode(t *testing.T) {
	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with state
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: "https://example.com/token",
	}
	stateJson, _ := json.Marshal(state)
	authSession.AddFlash(stateJson)
	err := authSession.Save(req, w)
	require.NoError(t, err)

	// Create DPoP session
	testDpopSession(t, DpopSessionOptions{
		PdsURL: "https://test.com",
		Issuer: stringPtr("https://example.com"),
	})

	// Set cookies from DPoP session creation
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// No code parameter - should fail during token exchange
	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCallbackHandler_TokenExchangeError(t *testing.T) {
	// Setup fake OAuth server that returns error
	fakeOAuthServer := fakeAuthServer(t, map[string]interface{}{
		"token-error": "invalid_grant",
	})
	defer fakeOAuthServer.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with state
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: fakeOAuthServer.URL + "/token",
	}
	stateJson, _ := json.Marshal(state)
	authSession.AddFlash(stateJson)
	err := authSession.Save(req, w)
	require.NoError(t, err)

	// Create DPoP session
	testDpopSession(t, DpopSessionOptions{
		PdsURL: "https://test.com",
		Issuer: stringPtr("https://example.com"),
	})

	// Set cookies from DPoP session creation
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("code", "invalid-code")
	q.Add("iss", "https://example.com")
	req.URL.RawQuery = q.Encode()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCallbackHandler_DPOPSessionError(t *testing.T) {
	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with state but no DPoP session
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: "https://example.com/token",
	}
	stateJson, _ := json.Marshal(state)
	authSession.AddFlash(stateJson)
	err := authSession.Save(req, w)
	require.NoError(t, err)

	// Add query parameters
	q := req.URL.Query()
	q.Add("code", "test-code")
	q.Add("iss", "https://example.com")
	req.URL.RawQuery = q.Encode()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCallbackHandler_SessionSaveError(t *testing.T) {
	// Setup fake OAuth server
	fakeOAuthServer := fakeAuthServer(t, nil)
	defer fakeOAuthServer.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with state
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: fakeOAuthServer.URL + "/token",
	}
	stateJson, _ := json.Marshal(state)
	authSession.AddFlash(stateJson)
	err := authSession.Save(req, w)
	require.NoError(t, err)

	// Create DPoP session
	testDpopSession(t, DpopSessionOptions{
		PdsURL: "https://test.com",
		Issuer: stringPtr("https://example.com"),
	})

	// Set cookies from DPoP session creation
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("code", "test-code")
	q.Add("iss", "https://example.com")
	req.URL.RawQuery = q.Encode()

	handler.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCallbackHandler_MethodAndPattern(t *testing.T) {
	handler := &callbackHandler{}
	require.Equal(t, "GET", handler.Method())
	require.Equal(t, "/auth-callback", handler.Pattern())
}

func TestCallbackHandler_CookieAttributes(t *testing.T) {
	// Setup fake OAuth server
	fakeOAuthServer := fakeAuthServer(t, nil)
	defer fakeOAuthServer.Close()

	oauthClient := testOAuthClient(t)
	sessionStore := sessions.NewCookieStore([]byte("test-key"))
	handler := setupTestCallbackHandler(t, oauthClient, sessionStore)

	req := httptest.NewRequest("GET", "/auth-callback", nil)
	w := httptest.NewRecorder()

	// Create auth session with state
	authSession, _ := sessionStore.New(req, SessionKeyAuth)
	state := &AuthorizeState{
		Verifier:      "test-verifier",
		State:         "test-state",
		TokenEndpoint: fakeOAuthServer.URL + "/token",
	}
	stateJson, _ := json.Marshal(state)
	authSession.AddFlash(stateJson)
	err := authSession.Save(req, w)
	require.NoError(t, err)

	// Create DPoP session
	testDpopSession(t, DpopSessionOptions{
		PdsURL: "https://test.com",
		Issuer: stringPtr("https://example.com"),
	})

	// Set cookies from DPoP session creation
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}

	// Add query parameters
	q := req.URL.Query()
	q.Add("code", "test-code")
	q.Add("iss", "https://example.com")
	req.URL.RawQuery = q.Encode()

	handler.ServeHTTP(w, req)

	// Verify cookie attributes
	cookies := w.Result().Cookies()
	for _, cookie := range cookies {
		if cookie.Name == "access_token" || cookie.Name == "refresh_token" ||
			cookie.Name == "handle" || cookie.Name == "did" {
			require.Equal(t, "/", cookie.Path, "Cookie %s should have root path", cookie.Name)
			require.Equal(t, http.SameSiteLaxMode, cookie.SameSite, "Cookie %s should have SameSiteLaxMode", cookie.Name)
		}
	}
}
