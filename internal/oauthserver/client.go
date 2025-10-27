package oauthserver

import (
	"strings"

	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/ory/fosite"
)

type client struct {
	auth.ClientMetadata
}

var _ fosite.Client = (*client)(nil)

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

// GetScopes implements fosite.Client.
func (c *client) GetScopes() fosite.Arguments {
	// Split the scope string by spaces to handle multiple scopes
	return strings.Split(c.Scope, " ")
}

// IsPublic implements fosite.Client.
func (c *client) IsPublic() bool {
	return true
}
