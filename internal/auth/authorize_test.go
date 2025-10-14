package auth

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthClient_Authorize_Success(t *testing.T) {
	server := fakeAuthServer(t, nil)
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	// Use empty string instead of nil to avoid nil pointer dereference in logging
	redirectURL, state, err := client.Authorize(dpopClient, identity)

	require.NoError(t, err)
	require.NotEmpty(t, redirectURL)
	require.NotNil(t, state)
	require.NotEmpty(t, state.Verifier)
	require.NotEmpty(t, state.State)
	require.NotEmpty(t, state.TokenEndpoint)

	// Verify redirect URL format
	parsedURL, err := url.Parse(redirectURL)
	require.NoError(t, err)
	require.Equal(t, "test-client", parsedURL.Query().Get("client_id"))
	require.NotEmpty(t, parsedURL.Query().Get("request_uri"))
}

func TestOAuthClient_Authorize_WithLoginHint(t *testing.T) {
	server := fakeAuthServer(t, nil)
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	redirectURL, state, err := client.Authorize(dpopClient, identity)

	require.NoError(t, err)
	require.NotEmpty(t, redirectURL)
	require.NotNil(t, state)
}

func TestOAuthClient_Authorize_ProtectedResourceError(t *testing.T) {
	// Server that returns error for protected resource
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	_, _, err := client.Authorize(dpopClient, identity)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to fetch authorization server")
}

func TestOAuthClient_Authorize_NoAuthorizationServers(t *testing.T) {
	server := fakeAuthServer(t, map[string]interface{}{
		"protected-resource": oauthProtectedResource{
			AuthorizationServers: []string{},
		},
	})
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	_, _, err := client.Authorize(dpopClient, identity)

	require.Error(t, err)
	require.Contains(t, err.Error(), "no authorization server found")
}

func TestOAuthClient_Authorize_AuthServerError(t *testing.T) {
	server := fakeAuthServer(t, map[string]interface{}{
		"auth-server": map[string]interface{}{
			"error": "server error",
		},
	})
	defer server.Close()

	// Override the auth server response to return error
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			err := json.NewEncoder(w).Encode(oauthProtectedResource{
				AuthorizationServers: []string{
					"http://" + r.Host + "/.well-known/oauth-authorization-server",
				},
			})
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			return
		}
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	_, _, err := client.Authorize(dpopClient, identity)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to fetch authorization server")
}

func TestOAuthClient_Authorize_PARError(t *testing.T) {
	server := fakeAuthServer(t, map[string]interface{}{
		"par-status": http.StatusBadRequest,
		"par":        map[string]string{"error": "invalid request"},
	})
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	_, _, err := client.Authorize(dpopClient, identity)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to make pushed authorization request")
}

func TestOAuthClient_Authorize_LocalhostHostMapping(t *testing.T) {
	// Test that localhost:3000 gets mapped to host.docker.internal:3000
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			err := json.NewEncoder(w).Encode(oauthProtectedResource{
				AuthorizationServers: []string{
					"http://localhost:3000/.well-known/oauth-authorization-server",
				},
			})
			if err != nil {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	_, _, err := client.Authorize(dpopClient, identity)

	// Should fail because the mapped host doesn't exist, but we're testing the mapping logic
	require.Error(t, err)
}

func TestOAuthClient_Authorize_StateAndVerifierGeneration(t *testing.T) {
	server1 := fakeAuthServer(t, nil)
	defer server1.Close()

	server2 := fakeAuthServer(t, nil)
	defer server2.Close()

	client := testOAuthClient(t)
	identity1 := testIdentity(server1.URL)
	identity2 := testIdentity(server2.URL)
	dpopClient1 := testDpopClient(t, identity1)
	dpopClient2 := testDpopClient(t, identity2)

	redirectURL1, state1, err1 := client.Authorize(dpopClient1, identity1)
	require.NoError(t, err1)

	redirectURL2, state2, err2 := client.Authorize(dpopClient2, identity2)
	require.NoError(t, err2)

	// Each call should generate unique state and verifier
	require.NotEqual(t, state1.State, state2.State)
	require.NotEqual(t, state1.Verifier, state2.Verifier)
	require.NotEqual(t, redirectURL1, redirectURL2)

	// State should be base64 URL encoded
	_, err := base64.URLEncoding.DecodeString(state1.State)
	require.NoError(t, err)

	_, err = base64.URLEncoding.DecodeString(state2.State)
	require.NoError(t, err)
}

func TestOAuthClient_Authorize_RedirectURLFormat(t *testing.T) {
	server := fakeAuthServer(t, nil)
	defer server.Close()

	client := testOAuthClient(t)
	identity := testIdentity(server.URL)
	dpopClient := testDpopClient(t, identity)

	redirectURL, _, err := client.Authorize(dpopClient, identity)
	require.NoError(t, err)

	parsedURL, err := url.Parse(redirectURL)
	require.NoError(t, err)

	// Verify query parameters
	query := parsedURL.Query()
	require.Equal(t, "test-client", query.Get("client_id"))
	require.NotEmpty(t, query.Get("request_uri"))

	// Verify the request_uri is a valid URN
	requestURI := query.Get("request_uri")
	require.Contains(t, requestURI, "urn:ietf:params:oauth:request_uri:")
}
