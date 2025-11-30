package oauthclient

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOAuthClient_RefreshToken_Success(t *testing.T) {
	server := fakeAuthServer(map[string]interface{}{
		"token": TokenResponse{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			Scope:        "atproto transition:generic",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		},
	})
	defer server.Close()

	client := testOAuthClient(t)
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenResp, err := client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.NoError(t, err)
	require.NotNil(t, tokenResp)
	require.Equal(t, "new-access-token", tokenResp.AccessToken)
	require.Equal(t, "new-refresh-token", tokenResp.RefreshToken)
	require.Equal(t, "atproto transition:generic", tokenResp.Scope)
	require.Equal(t, "Bearer", tokenResp.TokenType)
	require.Equal(t, 3600, tokenResp.ExpiresIn)
}

func TestOAuthClient_RefreshToken_ProtectedResourceError(t *testing.T) {
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
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	_, err = client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to fetch authorization server")
}

func TestOAuthClient_RefreshToken_NoAuthorizationServers(t *testing.T) {
	server := fakeAuthServer(map[string]interface{}{
		"protected-resource": oauthProtectedResource{
			AuthorizationServers: []string{},
		},
	})
	defer server.Close()

	client := testOAuthClient(t)
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	_, err = client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.Error(t, err)
	require.Contains(t, err.Error(), "no authorization server found")
}

func TestOAuthClient_RefreshToken_AuthServerError(t *testing.T) {
	server := fakeAuthServer(map[string]interface{}{
		"auth-server": map[string]interface{}{
			"error": "server error",
		},
	})
	defer server.Close()

	// Override the auth server response to return error
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/.well-known/oauth-protected-resource" {
			_ = json.NewEncoder(w).Encode(oauthProtectedResource{
				AuthorizationServers: []string{
					"http://" + r.Host + "/.well-known/oauth-authorization-server",
				},
			})
			return
		}
		if r.URL.Path == "/.well-known/oauth-authorization-server" {
			http.Error(w, "server error", http.StatusInternalServerError)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	})

	client := testOAuthClient(t)
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	_, err = client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to fetch authorization server")
}

func TestOAuthClient_RefreshToken_HTTPRequestError(t *testing.T) {
	// Create a server that doesn't respond to simulate network error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Don't respond, just close the connection
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			require.NoError(t, conn.Close())
		}
	}))
	defer server.Close()

	client := testOAuthClient(t)
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	_, err = client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.Error(t, err)
	require.Contains(t, err.Error(), "EOF")
}

func TestOAuthClient_RefreshToken_NonOKStatus(t *testing.T) {
	server := fakeAuthServer(map[string]interface{}{
		"token-status": http.StatusBadRequest,
		"token-error":  "invalid_grant",
	})
	defer server.Close()

	client := testOAuthClient(t)
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	_, err = client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to exchange code")
	require.Contains(t, err.Error(), "400 Bad Request")
}

func TestOAuthClient_RefreshToken_RequestParameters(t *testing.T) {
	var capturedRequest *http.Request
	server := fakeAuthServer(map[string]interface{}{
		"token": TokenResponse{
			AccessToken:  "new-access-token",
			RefreshToken: "new-refresh-token",
			Scope:        "atproto transition:generic",
			TokenType:    "Bearer",
			ExpiresIn:    3600,
		},
	})
	defer server.Close()

	// Override the token endpoint to capture and validate the request
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource":
			authServerURL := "http://" + r.Host + "/.well-known/oauth-authorization-server"
			_ = json.NewEncoder(w).Encode(oauthProtectedResource{
				AuthorizationServers: []string{authServerURL},
			})

		case "/.well-known/oauth-authorization-server":
			_ = json.NewEncoder(w).Encode(oauthAuthorizationServer{
				Issuer:        "https://example.com",
				TokenEndpoint: "http://" + r.Host + "/token",
				PAREndpoint:   "http://" + r.Host + "/par",
				AuthEndpoint:  "http://" + r.Host + "/auth",
			})

		case "/token":
			capturedRequest = r

			// Parse form to check parameters
			err := r.ParseForm()
			if err != nil {
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}

			// Verify required parameters are present
			requiredParams := []string{
				"client_id",
				"grant_type",
				"refresh_token",
				"client_assertion_type",
				"client_assertion",
			}
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
			if r.FormValue("grant_type") != "refresh_token" {
				http.Error(w, "invalid grant_type", http.StatusBadRequest)
				return
			}
			if r.FormValue("refresh_token") != "old-refresh-token" {
				http.Error(w, "invalid refresh_token", http.StatusBadRequest)
				return
			}
			if r.FormValue(
				"client_assertion_type",
			) != "urn:ietf:params:oauth:client-assertion-type:jwt-bearer" {
				http.Error(w, "invalid client_assertion_type", http.StatusBadRequest)
				return
			}
			if r.FormValue("client_assertion") == "" {
				http.Error(w, "missing client_assertion", http.StatusBadRequest)
				return
			}

			// Return success response
			_ = json.NewEncoder(w).Encode(TokenResponse{
				AccessToken:  "new-access-token",
				RefreshToken: "new-refresh-token",
				Scope:        "atproto transition:generic",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
			})

		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	client := testOAuthClient(t)
	dpopSession := testDpopSession(t, DpopSessionOptions{
		PdsURL: server.URL,
		Issuer: stringPtr("https://example.com"),
		TokenInfo: &TokenResponse{
			RefreshToken: "old-refresh-token",
		},
	})
	dpopClient := testDpopClientFromSession(t, dpopSession)

	identity, exists, err := dpopSession.GetIdentity()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	issuer, exists, err := dpopSession.GetIssuer()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenInfo, exists, err := dpopSession.GetTokenInfo()
	if err != nil {
		t.Fatal(err)
	}
	require.True(t, exists)
	tokenResp, err := client.RefreshToken(dpopClient, identity, issuer, tokenInfo.RefreshToken)

	require.NoError(t, err)
	require.NotNil(t, tokenResp)
	require.NotNil(t, capturedRequest)
	require.Equal(
		t,
		"application/x-www-form-urlencoded",
		capturedRequest.Header.Get("Content-Type"),
	)
	require.Equal(t, "POST", capturedRequest.Method)
}
