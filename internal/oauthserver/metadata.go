package oauthserver

import (
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

// authServerMetadata extends indigo's atproto-focused AuthServerMetadata with
// the registration_endpoint field (RFC 7591), which that struct omits.
// Dynamic client registration is what lets generic OAuth/MCP clients — which
// don't support atproto's Client ID Metadata Document convention — obtain a
// client_id usable against this same authorization server.
type authServerMetadata struct {
	oauth.AuthServerMetadata
	RegistrationEndpoint string `json:"registration_endpoint,omitempty"`
}

// buildAuthServerMetadata assembles the authorization-server metadata document
// for the given issuer origin. The advertised capabilities describe the target
// atproto-compliant surface; PAR and DPoP enforcement are wired up in later
// phases.
func buildAuthServerMetadata(issuer string) authServerMetadata {
	return authServerMetadata{
		AuthServerMetadata: oauth.AuthServerMetadata{
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
		},
		RegistrationEndpoint: issuer + "/oauth/register",
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
