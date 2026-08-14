package oauthserver

import (
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/go-jose/go-jose/v3"
	"github.com/ory/fosite"
)

type client struct {
	*oauth.ClientMetadata
}

var (
	_ fosite.Client              = (*client)(nil)
	_ fosite.ResponseModeClient  = (*client)(nil)
	_ fosite.OpenIDConnectClient = (*client)(nil)
)

// GetAudience implements fosite.Client.
func (c *client) GetAudience() fosite.Arguments {
	// For public clients, audience is typically empty or matches the client URI
	return fosite.Arguments{}
}

// GetGrantTypes implements fosite.Client.
func (c *client) GetGrantTypes() fosite.Arguments {
	return c.GrantTypes
}

// GetHashedSecret implements fosite.Client.
func (c *client) GetHashedSecret() []byte {
	// Public clients don't have secrets
	return nil
}

// GetID implements fosite.Client.
func (c *client) GetID() string {
	return c.ClientID
}

// GetRedirectURIs implements fosite.Client.
func (c *client) GetRedirectURIs() []string {
	return c.RedirectURIs
}

// GetResponseTypes implements fosite.Client.
func (c *client) GetResponseTypes() fosite.Arguments {
	return c.ResponseTypes
}

// GetResponseModes implements fosite.ResponseModeClient. The atproto OAuth
// client always sends an explicit response_mode; without this method fosite
// rejects the request as unsupported_response_mode. We allow the query and
// fragment modes (browser clients use query) plus the default.
func (c *client) GetResponseModes() []fosite.ResponseModeType {
	return []fosite.ResponseModeType{
		fosite.ResponseModeDefault,
		fosite.ResponseModeQuery,
		fosite.ResponseModeFragment,
	}
}

// GetScopes implements fosite.Client.
func (c *client) GetScopes() fosite.Arguments {
	// Split the scope string by spaces to handle multiple scopes
	return strings.Split(c.Scope, " ")
}

// IsPublic implements fosite.Client.
func (c *client) IsPublic() bool {
	return true
}

// GetRequestURIs implements fosite.OpenIDConnectClient.
func (c *client) GetRequestURIs() []string {
	return nil
}

// GetJSONWebKeys implements fosite.OpenIDConnectClient.
func (c *client) GetJSONWebKeys() *jose.JSONWebKeySet {
	if c.JWKS == nil || len(c.JWKS.Keys) == 0 {
		return nil
	}
	var set jose.JSONWebKeySet
	for _, key := range c.JWKS.Keys {
		if key.KeyID == nil {
			continue
		}
		converted, err := atcryptoJWKtoJose(key)
		if err != nil {
			continue
		}
		if converted.Use == "" {
			converted.Use = "sig"
		}
		set.Keys = append(set.Keys, *converted)
	}
	if len(set.Keys) == 0 {
		return nil
	}
	return &set
}

// GetJSONWebKeysURI implements fosite.OpenIDConnectClient.
func (c *client) GetJSONWebKeysURI() string {
	if c.JWKSURI != nil {
		return *c.JWKSURI
	}
	return ""
}

// GetRequestObjectSigningAlgorithm implements fosite.OpenIDConnectClient.
func (c *client) GetRequestObjectSigningAlgorithm() string {
	return ""
}

// GetTokenEndpointAuthMethod implements fosite.OpenIDConnectClient.
func (c *client) GetTokenEndpointAuthMethod() string {
	if c.TokenEndpointAuthMethod == "" {
		return "none"
	}
	return c.TokenEndpointAuthMethod
}

// GetTokenEndpointAuthSigningAlgorithm implements fosite.OpenIDConnectClient.
func (c *client) GetTokenEndpointAuthSigningAlgorithm() string {
	if c.TokenEndpointAuthSigningAlg != nil {
		return *c.TokenEndpointAuthSigningAlg
	}
	return ""
}
