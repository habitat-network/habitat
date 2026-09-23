// Package nango is a thin client for the parts of Nango's backend HTTP API
// (https://nango.dev/docs/reference/backend/http-api) that the MCP gateway
// uses: creating/deleting Integrations backed by Nango's "mcp-generic"
// provider (which discovers and registers with an MCP server's own
// authorization server per the MCP authorization spec), starting Connect
// sessions for end users, and deleting Connections.
package nango

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

const defaultBaseURL = "https://api.nango.dev"

// mcpGenericProvider is Nango's built-in provider that discovers and
// registers with an MCP server's authorization server on the caller's
// behalf, per https://nango.dev/docs/guides/auth/mcp-auth.
const mcpGenericProvider = "mcp-generic"

// connectionTypeTag tags every connection this client creates as an MCP
// connection, so they can be told apart from any other kind of connection
// (e.g. a future non-MCP integration) sharing the same Nango environment.
const connectionTypeTag = "mcp"

// Client is a client for Nango's backend HTTP API.
type Client struct {
	baseURL    string
	secretKey  string
	httpClient *http.Client
}

// NewClient constructs a Client authenticating with secretKey (an
// environment's Nango secret key).
func NewClient(secretKey string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &Client{baseURL: defaultBaseURL, secretKey: secretKey, httpClient: httpClient}
}

// CreateIntegration creates a Nango Integration backed by the mcp-generic
// provider, identified by uniqueKey (the caller's own MCP server ID).
func (c *Client) CreateIntegration(ctx context.Context, uniqueKey string) error {
	_, err := c.do(ctx, http.MethodPost, "/integrations", map[string]any{
		"unique_key": uniqueKey,
		"provider":   mcpGenericProvider,
	})
	return err
}

// DeleteIntegration deletes a Nango Integration.
func (c *Client) DeleteIntegration(ctx context.Context, uniqueKey string) error {
	_, err := c.do(ctx, http.MethodDelete, "/integrations/"+uniqueKey, nil)
	return err
}

// CreateConnectSession starts a Nango Connect session scoping the caller to
// the Integration identified by uniqueKey, tagging it with endUserID and
// orgID for correlation. It returns a session token for the frontend's
// Nango Connect UI.
func (c *Client) CreateConnectSession(
	ctx context.Context,
	uniqueKey string,
	endUserID, orgID string,
) (string, error) {
	body, err := c.do(ctx, http.MethodPost, "/connect/sessions", map[string]any{
		"allowed_integrations": []string{uniqueKey},
		"tags": map[string]string{
			"end_user_id":     endUserID,
			"organization_id": orgID,
			"type":            connectionTypeTag,
		},
	})
	if err != nil {
		return "", err
	}
	var out struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("decode create connect session response: %w", err)
	}
	return out.Data.Token, nil
}

// Connection identifies a single Nango Connection.
type Connection struct {
	ConnectionID      string
	ProviderConfigKey string
	// OrgID is the organization_id tag set on the connection by
	// CreateConnectSession, identifying which org's server it connects to.
	OrgID string
}

// ListConnections lists the MCP connections tagged with endUserID (see
// CreateConnectSession), across all Integrations.
func (c *Client) ListConnections(ctx context.Context, endUserID string) ([]Connection, error) {
	q := url.Values{}
	q.Set("tags[end_user_id]", endUserID)
	q.Set("tags[type]", connectionTypeTag)
	body, err := c.do(ctx, http.MethodGet, "/connections?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Connections []struct {
			ConnectionID      string            `json:"connection_id"`
			ProviderConfigKey string            `json:"provider_config_key"`
			Tags              map[string]string `json:"tags"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode list connections response: %w", err)
	}
	connections := make([]Connection, len(out.Connections))
	for i, conn := range out.Connections {
		connections[i] = Connection{
			ConnectionID:      conn.ConnectionID,
			ProviderConfigKey: conn.ProviderConfigKey,
			OrgID:             conn.Tags["organization_id"],
		}
	}
	return connections, nil
}

// ConnectionDetails is what's needed to call an MCP server on behalf of a
// connected user: its URL (entered by the user in Nango's Connect UI, held
// in connection_config) and, if the server requires authorization, a bearer
// access token Nango has obtained and keeps refreshed.
type ConnectionDetails struct {
	MCPServerURL string
	AccessToken  string // empty for connections that required no authorization
}

// GetConnection fetches a Connection's live details, including its
// credentials, from Nango.
func (c *Client) GetConnection(ctx context.Context, connectionID, providerConfigKey string) (*ConnectionDetails, error) {
	path := "/connection/" + connectionID + "?provider_config_key=" + providerConfigKey
	body, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		ConnectionConfig struct {
			MCPServerURL string `json:"mcp_server_url"`
		} `json:"connection_config"`
		Credentials struct {
			AccessToken string `json:"access_token"`
		} `json:"credentials"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode get connection response: %w", err)
	}
	if out.ConnectionConfig.MCPServerURL == "" {
		return nil, fmt.Errorf("connection %s has no mcp_server_url", connectionID)
	}
	return &ConnectionDetails{
		MCPServerURL: out.ConnectionConfig.MCPServerURL,
		AccessToken:  out.Credentials.AccessToken,
	}, nil
}

// DeleteConnection deletes a Nango Connection, revoking its stored credentials.
func (c *Client) DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error {
	path := "/connection/" + connectionID + "?provider_config_key=" + providerConfigKey
	_, err := c.do(ctx, http.MethodDelete, path, nil)
	return err
}

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.secretKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nango request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read nango response: %w", err)
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("nango request failed with status %d: %s", resp.StatusCode, respBody)
	}
	return respBody, nil
}
