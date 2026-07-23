package oauthserver

import (
	"strings"

	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/ory/fosite"
)

type client struct {
	*pdsclient.ClientMetadata
}

var _ fosite.Client = (*client)(nil)
var _ fosite.ResponseModeClient = (*client)(nil)

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
	return c.ClientId
}

// GetRedirectURIs implements fosite.Client.
func (c *client) GetRedirectURIs() []string {
	return c.RedirectUris
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
