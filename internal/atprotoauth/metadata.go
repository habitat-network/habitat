// Package atprotoauth is habitat's atproto-spec OAuth authorization server.
//
// It is built as a sibling of [internal/oauthserver] rather than a modification
// of it: the existing fosite-backed server is shaped around habitat acting as
// an OAuth client to the user's PDS, while the atproto spec requires
// URL-identified client registration, PAR-first authorization, asymmetric AS
// signing keys, and other deviations that don't compose cleanly onto fosite.
// Both servers will coexist on the same host while we build out and migrate
// (see route grouping in cmd/pear/main.go).
package atprotoauth

import (
	"encoding/json"
	"net/http"
)

// RoutePrefix is the URL prefix under which this server's endpoints are mounted.
// Keeping it distinct from the legacy /oauth/* prefix lets both servers coexist
// while we migrate. Once atprotoauth becomes the canonical implementation we'll
// drop the prefix and serve at /oauth/*.
const RoutePrefix = "/atproto-oauth"

// authorizationServerMetadata is the RFC 8414 metadata document advertised at
// /.well-known/oauth-authorization-server. atproto OAuth layers additional
// requirements on top of RFC 8414; see https://atproto.com/specs/oauth.
//
// Fields are encoded only when set (omitempty) so that this struct can grow as
// habitat implements more of the spec (PAR, JWKS, private_key_jwt, etc.)
// without forcing every release to advertise endpoints it doesn't serve yet.
type authorizationServerMetadata struct {
	Issuer                                     string   `json:"issuer"`
	AuthorizationEndpoint                      string   `json:"authorization_endpoint"`
	TokenEndpoint                              string   `json:"token_endpoint"`
	PushedAuthorizationRequestEndpoint         string   `json:"pushed_authorization_request_endpoint,omitempty"`
	RequirePushedAuthorizationRequests         bool     `json:"require_pushed_authorization_requests"`
	JWKSURI                                    string   `json:"jwks_uri,omitempty"`
	ScopesSupported                            []string `json:"scopes_supported"`
	ResponseTypesSupported                     []string `json:"response_types_supported"`
	GrantTypesSupported                        []string `json:"grant_types_supported"`
	CodeChallengeMethodsSupported              []string `json:"code_challenge_methods_supported"`
	TokenEndpointAuthMethodsSupported          []string `json:"token_endpoint_auth_methods_supported"`
	TokenEndpointAuthSigningAlgValuesSupported []string `json:"token_endpoint_auth_signing_alg_values_supported,omitempty"`
	DPoPSigningAlgValuesSupported              []string `json:"dpop_signing_alg_values_supported"`
	AuthorizationResponseIssParameterSupported bool     `json:"authorization_response_iss_parameter_supported"`
	SubjectTypesSupported                      []string `json:"subject_types_supported"`
	ClientIDMetadataDocumentSupported          bool     `json:"client_id_metadata_document_supported"`
}

// protectedResourceMetadata is the RFC 9728 metadata document advertised at
// /.well-known/oauth-protected-resource. It tells a client which authorization
// server(s) can issue tokens for this resource.
type protectedResourceMetadata struct {
	Resource                      string   `json:"resource"`
	AuthorizationServers          []string `json:"authorization_servers"`
	ScopesSupported               []string `json:"scopes_supported"`
	BearerMethodsSupported        []string `json:"bearer_methods_supported"`
	DPoPBoundAccessTokensRequired bool     `json:"dpop_bound_access_tokens_required"`
}

// ServeAuthorizationServerMetadata returns a handler that serves the AS
// discovery document at /.well-known/oauth-authorization-server. issuer must be
// the canonical https URL of this server (no trailing slash, no path) — it is
// also the value used as the `iss` claim in tokens this server issues, so
// clients reject any mismatch. The endpoints advertised here point at the
// atproto-spec server's RoutePrefix, not at the legacy /oauth/* routes.
//
// TODO: advertise pushed_authorization_request_endpoint + jwks_uri once those
// endpoints are implemented, and flip RequirePushedAuthorizationRequests to
// true (atproto OAuth requires PAR).
func ServeAuthorizationServerMetadata(issuer string) http.HandlerFunc {
	doc := authorizationServerMetadata{
		Issuer:                                     issuer,
		AuthorizationEndpoint:                      issuer + RoutePrefix + "/authorize",
		TokenEndpoint:                              issuer + RoutePrefix + "/token",
		RequirePushedAuthorizationRequests:         false,
		ScopesSupported:                            []string{"atproto", "transition:generic"},
		ResponseTypesSupported:                     []string{"code"},
		GrantTypesSupported:                        []string{"authorization_code", "refresh_token"},
		CodeChallengeMethodsSupported:              []string{"S256"},
		TokenEndpointAuthMethodsSupported:          []string{"none"},
		DPoPSigningAlgValuesSupported:              []string{"ES256", "ES256K"},
		AuthorizationResponseIssParameterSupported: true,
		SubjectTypesSupported:                      []string{"public"},
		ClientIDMetadataDocumentSupported:          true,
	}
	body, err := json.Marshal(doc)
	if err != nil {
		// The struct is fully static and json.Marshal of well-typed fields cannot fail.
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(body)
	}
}

// ServeProtectedResourceMetadata returns a handler that serves the resource
// server discovery document at /.well-known/oauth-protected-resource. Today
// the AS and the resource server are the same host, so authorizationServer
// will typically equal the resource URL.
func ServeProtectedResourceMetadata(resource, authorizationServer string) http.HandlerFunc {
	doc := protectedResourceMetadata{
		Resource:                      resource,
		AuthorizationServers:          []string{authorizationServer},
		ScopesSupported:               []string{"atproto", "transition:generic"},
		BearerMethodsSupported:        []string{"header"},
		DPoPBoundAccessTokensRequired: true,
	}
	body, err := json.Marshal(doc)
	if err != nil {
		panic(err)
	}
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "max-age=3600")
		_, _ = w.Write(body)
	}
}
