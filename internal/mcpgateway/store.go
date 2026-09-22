// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members opt in to authorizing with
// those servers. Authorization is brokered through Nango
// (https://nango.dev/docs/guides/auth/mcp-auth): Nango discovers and
// registers with a server's own authorization server per the MCP
// authorization spec and holds the resulting OAuth tokens, so this package
// only tracks which of an org's members have connected to which server.
package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

var (
	// ErrServerNotFound is returned when no MCP server matches the given ID/org.
	ErrServerNotFound = errors.New("mcp server not found")
	// ErrCredentialNotFound is returned when a user has no stored connection for a server.
	ErrCredentialNotFound = errors.New("mcp server connection not found")
	// ErrNotOAuthServer is returned when starting authorization against a server that
	// doesn't require it.
	ErrNotOAuthServer = errors.New("mcp server does not require authorization")
)

// NangoClient is the subset of Nango's backend API the gateway needs. See
// internal/nango.Client for the concrete implementation.
type NangoClient interface {
	// CreateIntegration creates a Nango Integration backed by the
	// mcp-generic provider, identified by uniqueKey.
	CreateIntegration(ctx context.Context, uniqueKey string) error
	// DeleteIntegration deletes a Nango Integration.
	DeleteIntegration(ctx context.Context, uniqueKey string) error
	// CreateConnectSession starts a Nango Connect session scoped to the
	// Integration identified by uniqueKey, returning a session token for
	// the frontend's Nango Connect UI.
	CreateConnectSession(ctx context.Context, uniqueKey string, endUserID, orgID string) (string, error)
	// DeleteConnection deletes a Nango Connection.
	DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error
}

// Store manages MCP server configuration and per-user Nango connections for orgs.
type Store interface {
	// AddServer configures a new MCP server for the org, probing it to detect
	// whether it requires authorization and, if so, creating a Nango
	// Integration for it.
	AddServer(ctx context.Context, orgID syntax.DID, name, url, description string) (*Server, error)
	// UpdateServer updates fields of an existing org MCP server. A nil field is
	// left unchanged. If url changes, the server is re-probed as in AddServer,
	// creating/deleting its Nango Integration if that flips whether it requires
	// authorization.
	UpdateServer(
		ctx context.Context,
		orgID syntax.DID,
		id ServerID,
		name, url, description *string,
	) (*Server, error)
	// RemoveServer deletes an org's MCP server, its Nango Integration (if any),
	// and any stored user connections for it.
	RemoveServer(ctx context.Context, orgID syntax.DID, id ServerID) error
	// ListServers lists the MCP servers configured for an org.
	ListServers(ctx context.Context, orgID syntax.DID) ([]*Server, error)
	// GetServer fetches a single MCP server by ID, scoped to the org.
	GetServer(ctx context.Context, orgID syntax.DID, id ServerID) (*Server, error)

	// StartAuthorization begins a Nango Connect session for did to connect to
	// an org's MCP server, returning a session token for the frontend's Nango
	// Connect UI.
	StartAuthorization(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		id ServerID,
	) (sessionToken string, err error)
	// ConfirmConnection records that did has connected to an MCP server via
	// the given Nango connection, as reported by the frontend after the
	// Nango Connect UI reports success.
	ConfirmConnection(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		id ServerID,
		connectionID string,
	) error

	// DisconnectServer deletes a user's Nango connection to an MCP server and
	// forgets it.
	DisconnectServer(ctx context.Context, did syntax.DID, id ServerID) error
	// IsConnected reports whether a user has a stored connection to an MCP server.
	IsConnected(ctx context.Context, did syntax.DID, id ServerID) (bool, error)
}

type store struct {
	db         *gorm.DB
	httpClient *http.Client
	nango      NangoClient
}

// NewStore constructs a Store backed by db. httpClient is used to probe MCP
// servers to detect whether they require authorization; nangoClient brokers
// the actual OAuth flow and token storage.
func NewStore(db *gorm.DB, httpClient *http.Client, nangoClient NangoClient) (Store, error) {
	if nangoClient == nil {
		return nil, fmt.Errorf("nango client is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if err := db.AutoMigrate(&serverModel{}, &credentialModel{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	return &store{db: db, httpClient: httpClient, nango: nangoClient}, nil
}

func (s *store) AddServer(
	ctx context.Context,
	orgID syntax.DID,
	name, url, description string,
) (*Server, error) {
	authType, err := s.detectAuth(ctx, url)
	if err != nil {
		return nil, err
	}

	m := serverModel{
		ID:          ServerID(uuid.NewString()),
		OrgID:       orgID,
		Name:        name,
		URL:         url,
		Description: description,
		AuthType:    authType,
	}
	if authType == AuthTypeOAuth {
		if err := s.nango.CreateIntegration(ctx, string(m.ID)); err != nil {
			return nil, fmt.Errorf("create nango integration: %w", err)
		}
	}
	if err := s.db.WithContext(ctx).Create(&m).Error; err != nil {
		return nil, fmt.Errorf("failed to create mcp server: %w", err)
	}
	return serverFromModel(m), nil
}

func (s *store) getServerModel(
	ctx context.Context,
	orgID syntax.DID,
	id ServerID,
) (*serverModel, error) {
	var m serverModel
	err := s.db.WithContext(ctx).
		Where("id = ? AND org_id = ?", id, orgID).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrServerNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get mcp server: %w", err)
	}
	return &m, nil
}

func (s *store) UpdateServer(
	ctx context.Context,
	orgID syntax.DID,
	id ServerID,
	name, url, description *string,
) (*Server, error) {
	m, err := s.getServerModel(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if name != nil {
		m.Name = *name
	}
	if description != nil {
		m.Description = *description
	}
	if url != nil && *url != m.URL {
		newAuthType, err := s.detectAuth(ctx, *url)
		if err != nil {
			return nil, err
		}
		m.URL = *url
		if newAuthType != m.AuthType {
			if newAuthType == AuthTypeOAuth {
				if err := s.nango.CreateIntegration(ctx, string(id)); err != nil {
					return nil, fmt.Errorf("create nango integration: %w", err)
				}
			} else {
				if err := s.nango.DeleteIntegration(ctx, string(id)); err != nil {
					return nil, fmt.Errorf("delete nango integration: %w", err)
				}
			}
			m.AuthType = newAuthType
		}
	}

	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, fmt.Errorf("failed to update mcp server: %w", err)
	}
	return serverFromModel(*m), nil
}

func (s *store) RemoveServer(ctx context.Context, orgID syntax.DID, id ServerID) error {
	m, err := s.getServerModel(ctx, orgID, id)
	if err != nil {
		return err
	}

	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(m).Error; err != nil {
			return fmt.Errorf("failed to remove mcp server: %w", err)
		}
		if err := tx.Where("server_id = ?", id).Delete(&credentialModel{}).Error; err != nil {
			return fmt.Errorf("failed to remove mcp server connections: %w", err)
		}
		return nil
	}); err != nil {
		return err
	}

	if m.AuthType == AuthTypeOAuth {
		if err := s.nango.DeleteIntegration(ctx, string(id)); err != nil {
			return fmt.Errorf("delete nango integration: %w", err)
		}
	}
	return nil
}

func (s *store) ListServers(ctx context.Context, orgID syntax.DID) ([]*Server, error) {
	var models []serverModel
	if err := s.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("created_at asc").
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("failed to list mcp servers: %w", err)
	}
	servers := make([]*Server, len(models))
	for i, m := range models {
		servers[i] = serverFromModel(m)
	}
	return servers, nil
}

func (s *store) GetServer(ctx context.Context, orgID syntax.DID, id ServerID) (*Server, error) {
	m, err := s.getServerModel(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	return serverFromModel(*m), nil
}

func (s *store) StartAuthorization(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	id ServerID,
) (string, error) {
	m, err := s.getServerModel(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if m.AuthType != AuthTypeOAuth {
		return "", ErrNotOAuthServer
	}
	token, err := s.nango.CreateConnectSession(ctx, string(id), did.String(), orgID.String())
	if err != nil {
		return "", fmt.Errorf("create nango connect session: %w", err)
	}
	return token, nil
}

func (s *store) ConfirmConnection(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	id ServerID,
	connectionID string,
) error {
	if _, err := s.getServerModel(ctx, orgID, id); err != nil {
		return err
	}
	cred := credentialModel{
		ServerID:          id,
		DID:               did,
		NangoConnectionID: connectionID,
	}
	if err := s.db.WithContext(ctx).Save(&cred).Error; err != nil {
		return fmt.Errorf("failed to save mcp server connection: %w", err)
	}
	return nil
}

func (s *store) getCredentialModel(
	ctx context.Context,
	did syntax.DID,
	id ServerID,
) (*credentialModel, error) {
	var cred credentialModel
	err := s.db.WithContext(ctx).
		Where("did = ? AND server_id = ?", did, id).
		First(&cred).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCredentialNotFound
	} else if err != nil {
		return nil, fmt.Errorf("failed to get mcp server connection: %w", err)
	}
	return &cred, nil
}

func (s *store) DisconnectServer(ctx context.Context, did syntax.DID, id ServerID) error {
	cred, err := s.getCredentialModel(ctx, did, id)
	if err != nil {
		return err
	}
	if err := s.nango.DeleteConnection(ctx, cred.NangoConnectionID, string(id)); err != nil {
		return fmt.Errorf("delete nango connection: %w", err)
	}
	if err := s.db.WithContext(ctx).Delete(cred).Error; err != nil {
		return fmt.Errorf("failed to remove mcp server connection: %w", err)
	}
	return nil
}

func (s *store) IsConnected(ctx context.Context, did syntax.DID, id ServerID) (bool, error) {
	var count int64
	if err := s.db.WithContext(ctx).
		Model(&credentialModel{}).
		Where("did = ? AND server_id = ?", did, id).
		Count(&count).Error; err != nil {
		return false, fmt.Errorf("failed to check mcp server connection: %w", err)
	}
	return count > 0, nil
}
