// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members opt in to authenticating with
// those servers by storing an encrypted per-user credential.
package mcpgateway

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/habitat-network/habitat/internal/encrypt"
	"gorm.io/gorm"
)

var (
	// ErrServerNotFound is returned when no MCP server matches the given ID/org.
	ErrServerNotFound = errors.New("mcp server not found")
	// ErrCredentialNotFound is returned when a user has no stored credential for a server.
	ErrCredentialNotFound = errors.New("mcp server credential not found")
	// ErrInvalidAuthType is returned when an unrecognized auth type is supplied.
	ErrInvalidAuthType = errors.New("invalid mcp server auth type")
)

// Store manages MCP server configuration and per-user credentials for orgs.
type Store interface {
	// AddServer configures a new MCP server for the org.
	AddServer(
		ctx context.Context,
		orgID syntax.DID,
		name, url, description string,
		authType AuthType,
	) (*Server, error)
	// UpdateServer updates fields of an existing org MCP server. A nil field is left unchanged.
	UpdateServer(
		ctx context.Context,
		orgID syntax.DID,
		id ServerID,
		name, url, description *string,
		authType *AuthType,
	) (*Server, error)
	// RemoveServer deletes an org's MCP server along with any stored user credentials for it.
	RemoveServer(ctx context.Context, orgID syntax.DID, id ServerID) error
	// ListServers lists the MCP servers configured for an org.
	ListServers(ctx context.Context, orgID syntax.DID) ([]*Server, error)
	// GetServer fetches a single MCP server by ID, scoped to the org.
	GetServer(ctx context.Context, orgID syntax.DID, id ServerID) (*Server, error)

	// ConnectServer stores (or replaces) a user's credential for an MCP server.
	ConnectServer(ctx context.Context, did syntax.DID, id ServerID, credential string) error
	// DisconnectServer removes a user's stored credential for an MCP server.
	DisconnectServer(ctx context.Context, did syntax.DID, id ServerID) error
	// IsConnected reports whether a user has a stored credential for an MCP server.
	IsConnected(ctx context.Context, did syntax.DID, id ServerID) (bool, error)
	// GetCredential returns a user's decrypted credential for an MCP server.
	GetCredential(ctx context.Context, did syntax.DID, id ServerID) (string, error)
}

type store struct {
	db            *gorm.DB
	encryptionKey []byte
}

// NewStore constructs a Store backed by db, encrypting stored user credentials with encryptionKey.
func NewStore(db *gorm.DB, encryptionKey []byte) (Store, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	if err := db.AutoMigrate(&serverModel{}, &credentialModel{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	return &store{db: db, encryptionKey: encryptionKey}, nil
}

func validateAuthType(authType AuthType) error {
	switch authType {
	case AuthTypeNone, AuthTypeAPIKey:
		return nil
	default:
		return fmt.Errorf("%w: %q", ErrInvalidAuthType, authType)
	}
}

func (s *store) AddServer(
	ctx context.Context,
	orgID syntax.DID,
	name, url, description string,
	authType AuthType,
) (*Server, error) {
	if err := validateAuthType(authType); err != nil {
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
	authType *AuthType,
) (*Server, error) {
	m, err := s.getServerModel(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if name != nil {
		m.Name = *name
	}
	if url != nil {
		m.URL = *url
	}
	if description != nil {
		m.Description = *description
	}
	if authType != nil {
		if err := validateAuthType(*authType); err != nil {
			return nil, err
		}
		m.AuthType = *authType
	}

	if err := s.db.WithContext(ctx).Save(m).Error; err != nil {
		return nil, fmt.Errorf("failed to update mcp server: %w", err)
	}
	return serverFromModel(*m), nil
}

func (s *store) RemoveServer(ctx context.Context, orgID syntax.DID, id ServerID) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND org_id = ?", id, orgID).Delete(&serverModel{})
		if res.Error != nil {
			return fmt.Errorf("failed to remove mcp server: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return ErrServerNotFound
		}
		if err := tx.Where("server_id = ?", id).Delete(&credentialModel{}).Error; err != nil {
			return fmt.Errorf("failed to remove mcp server credentials: %w", err)
		}
		return nil
	})
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

func (s *store) ConnectServer(
	ctx context.Context,
	did syntax.DID,
	id ServerID,
	credential string,
) error {
	encrypted, err := encrypt.EncryptCBOR(credential, s.encryptionKey)
	if err != nil {
		return fmt.Errorf("failed to encrypt credential: %w", err)
	}
	m := credentialModel{
		ServerID:   id,
		DID:        did,
		Credential: encrypted,
	}
	if err := s.db.WithContext(ctx).Save(&m).Error; err != nil {
		return fmt.Errorf("failed to save mcp server credential: %w", err)
	}
	return nil
}

func (s *store) DisconnectServer(ctx context.Context, did syntax.DID, id ServerID) error {
	res := s.db.WithContext(ctx).
		Where("did = ? AND server_id = ?", did, id).
		Delete(&credentialModel{})
	if res.Error != nil {
		return fmt.Errorf("failed to remove mcp server credential: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrCredentialNotFound
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

func (s *store) GetCredential(ctx context.Context, did syntax.DID, id ServerID) (string, error) {
	var m credentialModel
	err := s.db.WithContext(ctx).
		Where("did = ? AND server_id = ?", did, id).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", ErrCredentialNotFound
	} else if err != nil {
		return "", fmt.Errorf("failed to get mcp server credential: %w", err)
	}

	var credential string
	if err := encrypt.DecryptCBOR(m.Credential, s.encryptionKey, &credential); err != nil {
		return "", fmt.Errorf("failed to decrypt credential: %w", err)
	}
	return credential, nil
}
