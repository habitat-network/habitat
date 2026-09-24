package oauthserver

import (
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

// buildAuthServerMetadata assembles the authorization-server metadata document
// for the given issuer origin. The advertised capabilities describe the target
// atproto-compliant surface; PAR and DPoP enforcement are wired up in later
// phases.
func buildAuthServerMetadata(issuer string) oauth.AuthServerMetadata {
	return oauth.AuthServerMetadata{
		Issuer:                             issuer,
		AuthorizationEndpoint:              issuer + "/oauth/authorize",
		TokenEndpoint:                      issuer + "/oauth/token",
		PushedAuthorizationRequestEndpoint: issuer + "/oauth/par",
		ResponseTypesSupported:             []string{"code"},
		GrantTypesSupported: []string{
			"authorization_code",
			"refresh_token",
			"urn:ietf:params:oauth:grant-type:jwt-bearer",
		},
		CodeChallengeMethodsSupported:              []string{"S256"},
		TokenEndpointAuthMethodsSupoorted:          []string{"none", "private_key_jwt"},
		TokenEndpointAuthSigningAlgValuesSupported: []string{"ES256"},
		ScopesSupported:                            []string{"atproto"},
		DPoPSigningAlgValuesSupported:              []string{"ES256"},
		AuthorizationReponseISSParameterSupported:  true,
		RequirePushedAuthorizationRequests:         true,
		ClientIDMetadataDocumentSupported:          true,
	}
}

// buildMCPAuthServerMetadata assembles the RFC 8414 authorization-server
// metadata document for the MCP endpoints. Unlike buildAuthServerMetadata,
// this advertises dynamic client registration instead of client-id metadata
// documents, and mandatory PKCE instead of PAR.
func buildMCPAuthServerMetadata(issuer string) map[string]any {
	mcpIssuer := issuer + MCPIssuerPath
	return map[string]any{
		"issuer":                                         mcpIssuer,
		"authorization_endpoint":                         issuer + MCPAuthorizePath,
		"token_endpoint":                                 issuer + MCPTokenPath,
		"registration_endpoint":                          issuer + MCPRegisterPath,
		"response_types_supported":                       []string{"code"},
		"grant_types_supported":                          []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":               []string{"S256"},
		"token_endpoint_auth_methods_supported":          []string{"none"},
		"authorization_response_iss_parameter_supported": true,
	}
}

// protectedResourceMetadata is the RFC 9728 protected-resource metadata
// document. We emit our own type rather than indigo's oauth.ProtectedResourceMetadata
// because that struct omits the required `resource` field: the atproto OAuth
// client rejects the document without it (it must exactly equal the resource
// origin) and never proceeds to discover the authorization server.
type protectedResourceMetadata struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
}

// buildProtectedResourceMetadata assembles the protected-resource metadata
// document. Habitat is both the resource server and the authorization server,
// so the resource identifier and the single authorization server are both the
// issuer origin.
func buildProtectedResourceMetadata(issuer string) protectedResourceMetadata {
	return protectedResourceMetadata{
		Resource:             issuer,
		AuthorizationServers: []string{issuer},
	}
}
