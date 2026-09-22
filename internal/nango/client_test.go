package nango

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClientCreateIntegration(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/integrations", r.URL.Path)
		require.Equal(t, "Bearer test-secret", r.Header.Get("Authorization"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{}})
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-secret", srv.Client())
	c.baseURL = srv.URL

	err := c.CreateIntegration(t.Context(), "server-123")
	require.NoError(t, err)
	require.Equal(t, "server-123", gotBody["unique_key"])
	require.Equal(t, "mcp-generic", gotBody["provider"])
}

func TestClientDeleteIntegration(t *testing.T) {
	var gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-secret", srv.Client())
	c.baseURL = srv.URL

	require.NoError(t, c.DeleteIntegration(t.Context(), "server-123"))
	require.Equal(t, http.MethodDelete, gotMethod)
	require.Equal(t, "/integrations/server-123", gotPath)
}

func TestClientCreateConnectSession(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/connect/sessions", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"token": "session-token-abc"},
		})
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-secret", srv.Client())
	c.baseURL = srv.URL

	token, err := c.CreateConnectSession(t.Context(), "server-123", "did:plc:user", "did:plc:org")
	require.NoError(t, err)
	require.Equal(t, "session-token-abc", token)
	require.Equal(t, []any{"server-123"}, gotBody["allowed_integrations"])
	tags, ok := gotBody["tags"].(map[string]any)
	require.True(t, ok)
	require.Equal(t, "did:plc:user", tags["end_user_id"])
	require.Equal(t, "did:plc:org", tags["organization_id"])
}

func TestClientDeleteConnection(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-secret", srv.Client())
	c.baseURL = srv.URL

	require.NoError(t, c.DeleteConnection(t.Context(), "conn-1", "server-123"))
	require.True(t, strings.HasPrefix(gotURL, "/connection/conn-1"))
	require.Contains(t, gotURL, "provider_config_key=server-123")
}

func TestClientErrorResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	t.Cleanup(srv.Close)
	c := NewClient("test-secret", srv.Client())
	c.baseURL = srv.URL

	err := c.CreateIntegration(t.Context(), "server-123")
	require.Error(t, err)
	require.Contains(t, err.Error(), "400")
}
