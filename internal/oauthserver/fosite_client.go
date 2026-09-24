package oauthserver

import (
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/ory/fosite"
)

// clientDisplay exposes the presentation fields the consent and
// connected-apps UI need, regardless of which fosite.Client implementation
// backs a given client_id: a client resolved from an atproto client-id
// metadata document (*client), or one registered locally through RFC 7591
// dynamic client registration (*dynamicClient).
type clientDisplay interface {
	displayName() string
	displayURI() string
	logoURI() string
	tosURI() string
	policyURI() string
}

type client struct {
	*oauth.ClientMetadata
}

var (
	_ fosite.Client              = (*client)(nil)
	_ fosite.ResponseModeClient  = (*client)(nil)
	_ fosite.OpenIDConnectClient = (*client)(nil)
	_ clientDisplay              = (*client)(nil)
)

func (c *client) displayName() string {
	if c.ClientName != nil {
		return *c.ClientName
	}
	return ""
}

func (c *client) displayURI() string {
	if c.ClientURI != nil {
		return *c.ClientURI
	}
	return ""
}

func (c *client) logoURI() string {
	if c.LogoURI != nil {
		return *c.LogoURI
	}
	return ""
}

func (c *client) tosURI() string {
	if c.TosURI != nil {
		return *c.TosURI
	}
	return ""
}

func (c *client) policyURI() string {
	if c.PolicyURI != nil {
		return *c.PolicyURI
	}
	return ""
}

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
		converted, err := clientmetadata.ConvertJWK(key)
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

// dynamicClient is the fosite.Client for a client registered through RFC 7591
// dynamic client registration (the MCP endpoints). Unlike client, it isn't
// resolved from a client-id metadata document: it's a public client, always
// PKCE-bound, whose metadata lives in RegisteredClient rows in our own
// storage.
type dynamicClient struct {
	*RegisteredClient
	redirectURIs []string
	grantTypes   []string
}

var (
	_ fosite.Client = (*dynamicClient)(nil)
	_ clientDisplay = (*dynamicClient)(nil)
)

// GetID implements fosite.Client.
func (c *dynamicClient) GetID() string { return c.ClientID }

// GetHashedSecret implements fosite.Client. Dynamically registered clients
// are always public.
func (c *dynamicClient) GetHashedSecret() []byte { return nil }

// GetRedirectURIs implements fosite.Client.
func (c *dynamicClient) GetRedirectURIs() []string { return c.redirectURIs }

// GetGrantTypes implements fosite.Client.
func (c *dynamicClient) GetGrantTypes() fosite.Arguments { return c.grantTypes }

// GetResponseTypes implements fosite.Client.
func (c *dynamicClient) GetResponseTypes() fosite.Arguments { return fosite.Arguments{"code"} }

// GetScopes implements fosite.Client. Dynamically registered clients may
// request any scope this server's ScopeStrategy grants.
func (c *dynamicClient) GetScopes() fosite.Arguments { return fosite.Arguments{scopeMCP} }

// GetAudience implements fosite.Client.
func (c *dynamicClient) GetAudience() fosite.Arguments { return fosite.Arguments{} }

// IsPublic implements fosite.Client.
func (c *dynamicClient) IsPublic() bool { return true }

func (c *dynamicClient) displayName() string { return c.ClientName }
func (c *dynamicClient) displayURI() string  { return "" }
func (c *dynamicClient) logoURI() string     { return "" }
func (c *dynamicClient) tosURI() string      { return "" }
func (c *dynamicClient) policyURI() string   { return "" }
