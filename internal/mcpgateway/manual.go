package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/net/http/httpguts"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/habitat-network/habitat/internal/encrypt"
)

// AuthType says how pear authenticates to an org's MCP server.
type AuthType string

const (
	// AuthTypeOAuth servers are brokered by Nango's mcp-generic connector:
	// each member signs in to the server with their own account, following
	// the MCP authorization spec.
	AuthTypeOAuth AuthType = "oauth"
	// AuthTypeManual servers are configured once by an org admin with a URL
	// and optional static headers (e.g. an API key), shared by every member.
	// This covers servers that need no auth or authenticate some other way
	// than MCP OAuth, which Nango's connector can't handle.
	AuthTypeManual AuthType = "manual"
)

// ErrManualServerNotFound is returned when no manual MCP server matches the
// given org and ID.
var ErrManualServerNotFound = errors.New("manual mcp server not found")

// ErrInvalidServerURL is returned when a manual server's URL isn't an
// absolute http(s) URL.
var ErrInvalidServerURL = errors.New("url must be an absolute http or https URL")

// ErrInvalidHeaderName is returned when a manual server's header name isn't
// a valid HTTP header field name.
var ErrInvalidHeaderName = errors.New("invalid header name")

// ManualServer is an MCP server an org admin configured by hand. Its URL and
// headers are only ever handed to pear's own MCP client (see
// internal/mcpserver), never back out through the API: a URL can itself be a
// credential (some providers embed the key in it), and headers usually carry
// one.
type ManualServer struct {
	OrgID syntax.DID
	// ID is the server's name, chosen once at creation. It shares a
	// namespace with the org's OAuth servers (see validateServerName).
	ID          syntax.RecordKey
	Description string
	URL         string
	Headers     map[string]string
}

func validateManualConfig(serverURL string, headers map[string]string) error {
	u, err := url.Parse(serverURL)
	if err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidServerURL
	}
	for name, value := range headers {
		if !httpguts.ValidHeaderFieldName(name) {
			return fmt.Errorf("%w: %q", ErrInvalidHeaderName, name)
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return fmt.Errorf("invalid value for header %q", name)
		}
	}
	return nil
}

// ManualServerStore persists manually configured MCP servers. They're kept
// in pear's own database rather than the org's members space, since their
// config holds secrets members shouldn't be able to read.
type ManualServerStore interface {
	// Put creates or replaces a manual server.
	Put(ctx context.Context, server *ManualServer) error
	// Get returns ErrManualServerNotFound if there's no such server.
	Get(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) (*ManualServer, error)
	List(ctx context.Context, orgID syntax.DID) ([]*ManualServer, error)
	// Delete returns ErrManualServerNotFound if there's no such server.
	Delete(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error
}

// manualServerModel is a manual server's row. URL and headers are stored
// together, encrypted, in Config.
type manualServerModel struct {
	OrgDID      string `gorm:"column:org_did;primaryKey"`
	ID          string `gorm:"column:id;primaryKey"`
	Description string
	Config      string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (manualServerModel) TableName() string { return "mcp_manual_servers" }

// manualServerConfig is the plaintext of manualServerModel.Config.
type manualServerConfig struct {
	URL     string
	Headers map[string]string
}

type manualServerStore struct {
	db            *gorm.DB
	encryptionKey []byte
}

// NewManualServerStore constructs a ManualServerStore backed by db,
// encrypting each server's URL and headers with encryptionKey.
func NewManualServerStore(db *gorm.DB, encryptionKey []byte) (ManualServerStore, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	if err := db.AutoMigrate(&manualServerModel{}); err != nil {
		return nil, fmt.Errorf("migrate manual mcp servers: %w", err)
	}
	return &manualServerStore{db: db, encryptionKey: encryptionKey}, nil
}

func (s *manualServerStore) Put(ctx context.Context, server *ManualServer) error {
	config, err := encrypt.EncryptCBOR(
		manualServerConfig{URL: server.URL, Headers: server.Headers},
		s.encryptionKey,
	)
	if err != nil {
		return fmt.Errorf("encrypt manual server config: %w", err)
	}
	// Upsert without touching created_at, which Save would zero on update.
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "org_did"}, {Name: "id"}},
		DoUpdates: clause.AssignmentColumns([]string{"description", "config", "updated_at"}),
	}).Create(&manualServerModel{
		OrgDID:      server.OrgID.String(),
		ID:          string(server.ID),
		Description: server.Description,
		Config:      config,
	}).Error
}

func (s *manualServerStore) fromModel(m *manualServerModel) (*ManualServer, error) {
	var config manualServerConfig
	if err := encrypt.DecryptCBOR(m.Config, s.encryptionKey, &config); err != nil {
		return nil, fmt.Errorf("decrypt manual server config: %w", err)
	}
	return &ManualServer{
		OrgID:       syntax.DID(m.OrgDID),
		ID:          syntax.RecordKey(m.ID),
		Description: m.Description,
		URL:         config.URL,
		Headers:     config.Headers,
	}, nil
}

func (s *manualServerStore) Get(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
) (*ManualServer, error) {
	var m manualServerModel
	err := s.db.WithContext(ctx).
		Where("org_did = ? AND id = ?", orgID.String(), string(id)).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrManualServerNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get manual server: %w", err)
	}
	return s.fromModel(&m)
}

func (s *manualServerStore) List(ctx context.Context, orgID syntax.DID) ([]*ManualServer, error) {
	var models []manualServerModel
	if err := s.db.WithContext(ctx).
		Where("org_did = ?", orgID.String()).
		Order("id").
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list manual servers: %w", err)
	}
	out := make([]*ManualServer, len(models))
	for i := range models {
		server, err := s.fromModel(&models[i])
		if err != nil {
			return nil, err
		}
		out[i] = server
	}
	return out, nil
}

func (s *manualServerStore) Delete(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
) error {
	res := s.db.WithContext(ctx).
		Where("org_did = ? AND id = ?", orgID.String(), string(id)).
		Delete(&manualServerModel{})
	if res.Error != nil {
		return fmt.Errorf("delete manual server: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrManualServerNotFound
	}
	return nil
}
