package oauthserver

// jwtBearerAllowedClients is a hardcoded allow-list of client IDs (client
// metadata document URLs, see
// https://atproto.com/specs/oauth#client-id-metadata-document) permitted to
// use the JWT Bearer grant (RFC 7523, urn:ietf:params:oauth:grant-type:jwt-bearer)
// to mint access tokens directly from a signed assertion, without a
// user-driven authorization flow. The assertion's "iss" claim must match one
// of these client IDs, and is verified against the JWKS published in that
// client's metadata document.
//
// TODO: replace with a persisted/dynamic registry once client registration
// for this grant type is supported.
var jwtBearerAllowedClients = map[string]struct{}{
	// "https://example.com/client-metadata.json": {},
}

func isJWTBearerClientAllowed(clientID string) bool {
	_, ok := jwtBearerAllowedClients[clientID]
	return ok
}
