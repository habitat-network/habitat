package mcpauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// maxTokenResponseBytes caps how much of a token endpoint's response is read.
const maxTokenResponseBytes = 1 << 20

// tokenResponse is a successful token endpoint response (RFC 6749 §5.1).
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// tokenError is an error response from a token endpoint (RFC 6749 §5.2).
type tokenError struct {
	Code        string `json:"error"`
	Description string `json:"error_description"`
}

func (e *tokenError) Error() string {
	if e.Description == "" {
		return "mcpauth: token endpoint error: " + e.Code
	}
	return "mcpauth: token endpoint error: " + e.Code + ": " + e.Description
}

// requestToken posts form to tokenURL, authenticating as client, and
// returns the tokens issued. Token requests are made directly rather than
// through golang.org/x/oauth2, whose refreshes can't carry the RFC 8707
// resource parameter the MCP spec requires on every token request.
func requestToken(
	ctx context.Context,
	httpClient *http.Client,
	tokenURL string,
	client registeredClient,
	form url.Values,
) (*tokenResponse, error) {
	switch client.AuthMethod {
	case "client_secret_post":
		form.Set("client_id", client.ClientID)
		form.Set("client_secret", client.ClientSecret)
	case "client_secret_basic":
	default:
		form.Set("client_id", client.ClientID)
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		tokenURL,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: build token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	if client.AuthMethod == "client_secret_basic" {
		// RFC 6749 §2.3.1: both are form-encoded before basic encoding.
		req.SetBasicAuth(url.QueryEscape(client.ClientID), url.QueryEscape(client.ClientSecret))
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("mcpauth: token request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxTokenResponseBytes))
	if err != nil {
		return nil, fmt.Errorf("mcpauth: read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var te tokenError
		if json.Unmarshal(body, &te) == nil && te.Code != "" {
			return nil, &te
		}
		return nil, fmt.Errorf("mcpauth: token endpoint returned status %d", resp.StatusCode)
	}
	var tr tokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("mcpauth: decode token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("mcpauth: token response has no access_token")
	}
	if tr.TokenType != "" && !strings.EqualFold(tr.TokenType, "bearer") {
		return nil, fmt.Errorf("mcpauth: unsupported token type %q", tr.TokenType)
	}
	return &tr, nil
}
