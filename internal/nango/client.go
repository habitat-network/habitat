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
)

const defaultBaseURL = "https://api.nango.dev"

// mcpGenericProvider is Nango's built-in provider that discovers and
// registers with an MCP server's authorization server on the caller's
// behalf, per https://nango.dev/docs/guides/auth/mcp-auth.
const mcpGenericProvider = "mcp-generic"

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
