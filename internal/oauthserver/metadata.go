package oauthserver

import (
	"encoding/json"
	"net/http"

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

// buildProtectedResourceMetadata assembles the protected-resource metadata
// document. Habitat is both the resource server and the authorization server,
// so the single authorization server is the issuer origin.
func buildProtectedResourceMetadata(issuer string) oauth.ProtectedResourceMetadata {
	return oauth.ProtectedResourceMetadata{
		AuthorizationServers: []string{issuer},
	}
}

func writeMetadataJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
