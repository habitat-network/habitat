package mcpgateway

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// AuthType describes how a user authorizes against an MCP server, as
// detected by probing it per the MCP authorization spec
// (https://modelcontextprotocol.io/docs/tutorials/security/authorization).
type AuthType string

const (
	// AuthTypeNone means the MCP server did not challenge an unauthenticated
	// request, so it requires no per-user authorization.
	AuthTypeNone AuthType = "none"
	// AuthTypeOAuth means the MCP server requires each user to complete an
	// OAuth authorization-code flow against its own authorization server.
	AuthTypeOAuth AuthType = "oauth"
)

// ServerID identifies an MCP server configured for an org.
type ServerID string

// serverModel is an MCP server configured by an org admin. When AuthType is
// AuthTypeOAuth, the OAuth* fields hold the configuration discovered and
// registered (RFC 8414/9728/7591) against the server's authorization server
// when it was added.
type serverModel struct {
	ID          ServerID   `gorm:"primaryKey"`
	OrgID       syntax.DID `gorm:"index;not null"`
	Name        string     `gorm:"not null"`
	URL         string     `gorm:"not null"`
	Description string
	AuthType    AuthType `gorm:"not null"`

	OAuthIssuer                string
	OAuthAuthorizationEndpoint string
	OAuthTokenEndpoint         string
	OAuthScopes                string // space-separated
	OAuthClientID              string
	OAuthClientSecret          string // encrypted; empty for public clients

	CreatedAt time.Time
	UpdatedAt time.Time
}

// credentialModel stores a user's encrypted OAuth tokens for an MCP server.
type credentialModel struct {
	ServerID     ServerID   `gorm:"primaryKey"`
	DID          syntax.DID `gorm:"column:did;primaryKey"`
	AccessToken  string     `gorm:"not null"` // encrypted
	RefreshToken string     // encrypted; empty if the server didn't issue one
	TokenType    string
	Expiry       time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}

// pendingAuthModel tracks an in-flight OAuth authorization-code flow between
// Store.StartAuthorization and the redirect back to Store.CompleteAuthorization.
type pendingAuthModel struct {
	State        string     `gorm:"primaryKey"`
	ServerID     ServerID   `gorm:"not null"`
	DID          syntax.DID `gorm:"column:did;not null"`
	OrgID        syntax.DID `gorm:"not null"`
	CodeVerifier string     `gorm:"not null"`
	// ReturnURL is where the caller's browser is sent once authorization
	// completes, as supplied to Store.StartAuthorization. Distinct from the
	// OAuth redirect_uri (this instance's own callback endpoint, registered
	// with the MCP server's authorization server).
	ReturnURL string `gorm:"not null"`

	CreatedAt time.Time
}

// Server is an MCP server configured for an org.
type Server struct {
	ID          ServerID
	OrgID       syntax.DID
	Name        string
	URL         string
	Description string
	AuthType    AuthType
}

func serverFromModel(m serverModel) *Server {
	return &Server{
		ID:          m.ID,
		OrgID:       m.OrgID,
		Name:        m.Name,
		URL:         m.URL,
		Description: m.Description,
		AuthType:    m.AuthType,
	}
}
