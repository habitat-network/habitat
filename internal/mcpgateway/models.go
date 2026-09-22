package mcpgateway

import (
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// AuthType describes how a user authenticates to an MCP server.
type AuthType string

const (
	// AuthTypeNone means the MCP server requires no per-user credential.
	AuthTypeNone AuthType = "none"
	// AuthTypeAPIKey means each user must supply their own bearer token/API key.
	AuthTypeAPIKey AuthType = "api_key"
)

// ServerID identifies an MCP server configured for an org.
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

// credentialModel stores a user's encrypted credential for an MCP server.
type credentialModel struct {
	ServerID   ServerID   `gorm:"primaryKey"`
	DID        syntax.DID `gorm:"column:did;primaryKey"`
	Credential string     `gorm:"not null"` // encrypted

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
