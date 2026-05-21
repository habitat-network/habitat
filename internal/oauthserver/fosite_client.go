package oauthserver

import (
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/ory/fosite"
)

type client struct {
	oauth.ClientMetadata
}

var _ fosite.Client = (*client)(nil)

// GetAudience implements fosite.Client.
func (c *client) GetAudience() fosite.Arguments {
	return fosite.Arguments{}
}

// GetGrantTypes implements fosite.Client.
func (c *client) GetGrantTypes() fosite.Arguments {
	return c.GrantTypes
}

// GetHashedSecret implements fosite.Client.
func (c *client) GetHashedSecret() []byte {
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

// GetScopes implements fosite.Client.
func (c *client) GetScopes() fosite.Arguments {
	return strings.Split(c.Scope, " ")
}

// IsPublic implements fosite.Client.
func (c *client) IsPublic() bool {
	return true
}
