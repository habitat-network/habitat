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
	// AuthTypeOAuth means the MCP server requires each user to authorize via
	// OAuth, brokered through Nango's mcp-generic provider.
	AuthTypeOAuth AuthType = "oauth"
)

// ServerID identifies an MCP server configured for an org. It also serves as
// the unique_key of the Nango Integration backing it, when AuthType is
// AuthTypeOAuth.
type ServerID string

// serverModel is an MCP server configured by an org admin.
type serverModel struct {
	ID          ServerID   `gorm:"primaryKey"`
	OrgID       syntax.DID `gorm:"index;not null"`
	Name        string     `gorm:"not null"`
	URL         string     `gorm:"not null"`
	Description string
	AuthType    AuthType `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
}

// credentialModel records that a user has connected to an MCP server via
// Nango. The actual OAuth tokens live in Nango, not here.
type credentialModel struct {
	ServerID          ServerID   `gorm:"primaryKey"`
	DID               syntax.DID `gorm:"column:did;primaryKey"`
	NangoConnectionID string     `gorm:"not null"`

	CreatedAt time.Time
	UpdatedAt time.Time
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
