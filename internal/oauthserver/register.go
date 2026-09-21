package oauthserver

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/httpx"
)

// registerRequest is the RFC 7591 Dynamic Client Registration request body.
// Only the fields this server acts on are modeled; unrecognized fields are
// ignored.
type registerRequest struct {
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name"`
	ClientURI               string   `json:"client_uri"`
	Scope                   string   `json:"scope"`
}

// registerResponse is the RFC 7591 registration response: the client's
// assigned client_id plus its (possibly defaulted) metadata.
type registerResponse struct {
	ClientID                string   `json:"client_id"`
	ClientIDIssuedAt        int64    `json:"client_id_issued_at"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientName              string   `json:"client_name,omitempty"`
	ClientURI               string   `json:"client_uri,omitempty"`
	Scope                   string   `json:"scope"`
}

// HandleRegister implements RFC 7591 Dynamic Client Registration at
// /oauth/register. Registration exists for OAuth clients that cannot publish
// an atproto Client ID Metadata Document (the mechanism atproto-native
// clients use instead, see GetClient) — notably generic MCP clients such as
// Claude Desktop or the MCP inspector. Registered clients are always public
// (no client_secret): they authenticate via PKCE, same as any other public
// client this server supports.
func (o *OAuthServer) HandleRegister(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to decode request body", err)
		return
	}
	if len(req.RedirectURIs) == 0 {
		httpx.WriteInvalidRequest(ctx, w, "redirect_uris is required", nil)
		return
	}

	clientID, err := generateClientID()
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to generate client id: %w", err))
		return
	}

	grantTypes := req.GrantTypes
	if len(grantTypes) == 0 {
		grantTypes = []string{"authorization_code", "refresh_token"}
	}
	responseTypes := req.ResponseTypes
	if len(responseTypes) == 0 {
		responseTypes = []string{"code"}
	}
	scope := req.Scope
	if scope == "" {
		scope = "atproto"
	}

	metadata := &oauth.ClientMetadata{
		ClientID:                clientID,
		RedirectURIs:            req.RedirectURIs,
		GrantTypes:              grantTypes,
		ResponseTypes:           responseTypes,
		TokenEndpointAuthMethod: "none",
		Scope:                   scope,
	}
	if req.ClientName != "" {
		metadata.ClientName = &req.ClientName
	}
	if req.ClientURI != "" {
		metadata.ClientURI = &req.ClientURI
	}

	if err := o.storage.CreateDynamicClient(ctx, metadata); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to register client: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, registerResponse{
		ClientID:                clientID,
		ClientIDIssuedAt:        time.Now().Unix(),
		RedirectURIs:            metadata.RedirectURIs,
		TokenEndpointAuthMethod: metadata.TokenEndpointAuthMethod,
		GrantTypes:              metadata.GrantTypes,
		ResponseTypes:           metadata.ResponseTypes,
		ClientName:              req.ClientName,
		ClientURI:               req.ClientURI,
		Scope:                   scope,
	})
}

func generateClientID() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "mcp-" + base64.RawURLEncoding.EncodeToString(b), nil
}
