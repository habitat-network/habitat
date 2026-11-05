// Package llmgateway lets org admins configure LLM model connections for their
// org, and lets org members call those models through one unified API.
//
// A connection names a provider (see [Provider]), the provider's model ID and,
// optionally, a base URL. It's stored as a row of pear's database, keyed by
// org and the connection's name, which is also the model ID clients request.
// The provider API key is stored on the same row, encrypted with
// [encrypt.EncryptCBOR], and is never returned through the API.
//
// Providers are reached through github.com/tmc/langchaingo, so every provider
// is called the same way (see [ModelFactory]). openai.go translates between
// that and the OpenAI Chat Completions wire format, which pear serves for each
// org.
package llmgateway

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/encrypt"
)

// Provider is the API pear calls a connection's model through.
type Provider string

const (
	// ProviderOpenAI is OpenAI's API, or any OpenAI-compatible API (such as
	// OpenRouter, Groq or vLLM) at the connection's base URL.
	ProviderOpenAI Provider = "openai"
	// ProviderAnthropic is Anthropic's Messages API.
	ProviderAnthropic Provider = "anthropic"
	// ProviderGoogle is Google's Gemini API.
	ProviderGoogle Provider = "google"
	// ProviderOllama is an Ollama server at the connection's base URL. It
	// takes no API key.
	ProviderOllama Provider = "ollama"
)

// Providers lists every supported Provider.
var Providers = []Provider{ProviderOpenAI, ProviderAnthropic, ProviderGoogle, ProviderOllama}

// needsAPIKey reports whether p's API rejects unauthenticated calls, so a
// connection to it can't work without a key.
func (p Provider) needsAPIKey() bool {
	return p != ProviderOllama
}

var (
	// ErrConnectionNotFound is returned when no connection matches the given
	// org and ID.
	ErrConnectionNotFound = errors.New("llm connection not found")
	// ErrInvalidName is returned when a connection name doesn't match
	// namePattern.
	ErrInvalidName = errors.New(
		"name must be 1-64 characters, using only letters, digits, dots, hyphens, and underscores",
	)
	// ErrNameTaken is returned when adding a connection whose name another
	// connection in the same org already uses.
	ErrNameTaken = errors.New("a connection with this name already exists for this org")
	// ErrInvalidProvider is returned for a provider not in [Providers].
	ErrInvalidProvider = errors.New("unsupported provider")
	// ErrMissingModel is returned when a connection names no model.
	ErrMissingModel = errors.New("model is required")
	// ErrInvalidBaseURL is returned for a base URL that isn't an absolute
	// http(s) URL, or a missing one for a provider that needs it.
	ErrInvalidBaseURL = errors.New("base URL must be an absolute http or https URL")
	// ErrMissingAPIKey is returned when a provider that needs an API key would
	// be left without one.
	ErrMissingAPIKey = errors.New("this provider requires an API key")
	// ErrAPIKeyNotSupported is returned when giving an API key to a provider
	// that takes none.
	ErrAPIKeyNotSupported = errors.New("this provider doesn't take an API key")
)

// namePattern restricts connection names to a limited, predictable character
// set: they double as the connection's ID and the model ID clients request.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func validateName(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}
	// "." and ".." match the pattern but would make confusing model IDs.
	if name == "." || name == ".." {
		return ErrInvalidName
	}
	return nil
}

func validateBaseURL(provider Provider, baseURL string) error {
	if baseURL == "" {
		if provider == ProviderOllama {
			return fmt.Errorf("%w: ollama needs its server's URL", ErrInvalidBaseURL)
		}
		return nil
	}
	u, err := url.Parse(baseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ErrInvalidBaseURL
	}
	return nil
}

// IsConfigError reports whether err is a validation error the caller should
// fix, rather than a failure of the store.
func IsConfigError(err error) bool {
	return errors.Is(err, ErrInvalidName) ||
		errors.Is(err, ErrNameTaken) ||
		errors.Is(err, ErrInvalidProvider) ||
		errors.Is(err, ErrMissingModel) ||
		errors.Is(err, ErrInvalidBaseURL) ||
		errors.Is(err, ErrMissingAPIKey) ||
		errors.Is(err, ErrAPIKeyNotSupported)
}

// ConnectionID identifies a connection within its org. It's equal to the
// connection's name, chosen once at creation and immutable afterward.
type ConnectionID string

// Connection is an org's LLM model connection, without its API key.
type Connection struct {
	ID          ConnectionID
	Name        string
	Description string
	Provider    Provider
	// Model is the provider's identifier for the model.
	Model string
	// BaseURL overrides the provider's default API URL. Empty means the
	// provider's default.
	BaseURL string
	// HasAPIKey reports whether an API key is stored for the connection.
	HasAPIKey bool
}

// NewConnection is a connection to add to an org.
type NewConnection struct {
	Name        string
	Description string
	Provider    Provider
	Model       string
	BaseURL     string
	// APIKey is stored encrypted. Empty stores none.
	APIKey string
}

// ConnectionUpdate is a partial update to a connection. Nil fields are left
// unchanged. Name and Provider are immutable.
type ConnectionUpdate struct {
	Description *string
	Model       *string
	BaseURL     *string
	// APIKey replaces the stored key. A pointer to "" deletes it.
	APIKey *string
}

// connectionRow is the GORM model for a connection.
type connectionRow struct {
	OrgDID      syntax.DID   `gorm:"column:org_did;primaryKey;size:255"`
	Name        ConnectionID `gorm:"column:name;primaryKey;size:64"`
	Description string       `gorm:"column:description;not null;default:''"`
	Provider    Provider     `gorm:"column:provider;not null"`
	Model       string       `gorm:"column:model;not null"`
	BaseURL     string       `gorm:"column:base_url;not null;default:''"`
	// APIKeyEnc is the API key encrypted with encrypt.EncryptCBOR, or empty
	// when there's none.
	APIKeyEnc string    `gorm:"column:api_key_enc;not null;default:''"`
	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (connectionRow) TableName() string { return "llm_connections" }

// Models returns the GORM models this package persists, for db.Migrate.
func Models() []any { return []any{&connectionRow{}} }

func (r *connectionRow) connection() *Connection {
	return &Connection{
		ID:          r.Name,
		Name:        string(r.Name),
		Description: r.Description,
		Provider:    r.Provider,
		Model:       r.Model,
		BaseURL:     r.BaseURL,
		HasAPIKey:   r.APIKeyEnc != "",
	}
}

// Store reads and writes org LLM connections in pear's database, with their
// API keys encrypted.
type Store struct {
	db       *gorm.DB
	key      []byte
	newModel ModelFactory
}

// NewStore returns a Store over db, encrypting API keys with key, which must
// be 32 bytes. newModel builds the model a connection calls.
func NewStore(db *gorm.DB, key []byte, newModel ModelFactory) (*Store, error) {
	// Probe the key now so a bad one fails at construction, not on first use.
	if _, err := encrypt.EncryptCBOR(nil, key); err != nil {
		return nil, fmt.Errorf("llmgateway: invalid encryption key: %w", err)
	}
	return &Store{db: db, key: key, newModel: newModel}, nil
}

// Add adds a connection to org, storing its API key, if any.
func (s *Store) Add(ctx context.Context, org syntax.DID, c NewConnection) (*Connection, error) {
	if err := validateName(c.Name); err != nil {
		return nil, err
	}
	if !slices.Contains(Providers, c.Provider) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidProvider, c.Provider)
	}
	if strings.TrimSpace(c.Model) == "" {
		return nil, ErrMissingModel
	}
	if err := validateBaseURL(c.Provider, c.BaseURL); err != nil {
		return nil, err
	}
	if err := validateAPIKey(c.Provider, c.APIKey); err != nil {
		return nil, err
	}
	apiKeyEnc, err := s.encryptAPIKey(c.APIKey)
	if err != nil {
		return nil, err
	}
	row := &connectionRow{
		OrgDID:      org,
		Name:        ConnectionID(c.Name),
		Description: c.Description,
		Provider:    c.Provider,
		Model:       c.Model,
		BaseURL:     c.BaseURL,
		APIKeyEnc:   apiKeyEnc,
	}
	err = s.db.WithContext(ctx).Create(row).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, ErrNameTaken
	} else if err != nil {
		return nil, fmt.Errorf("insert llm connection: %w", err)
	}
	return row.connection(), nil
}

func validateAPIKey(provider Provider, apiKey string) error {
	if apiKey == "" && provider.needsAPIKey() {
		return ErrMissingAPIKey
	}
	if apiKey != "" && !provider.needsAPIKey() {
		return ErrAPIKeyNotSupported
	}
	return nil
}

// Update applies update to org's connection id. It returns
// [ErrConnectionNotFound] if there's no such connection.
func (s *Store) Update(
	ctx context.Context,
	org syntax.DID,
	id ConnectionID,
	update ConnectionUpdate,
) (*Connection, error) {
	var out *Connection
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := getRow(tx, org, id)
		if err != nil {
			return err
		}
		if update.Description != nil {
			row.Description = *update.Description
		}
		if update.Model != nil {
			if strings.TrimSpace(*update.Model) == "" {
				return ErrMissingModel
			}
			row.Model = *update.Model
		}
		if update.BaseURL != nil {
			if err := validateBaseURL(row.Provider, *update.BaseURL); err != nil {
				return err
			}
			row.BaseURL = *update.BaseURL
		}
		if update.APIKey != nil {
			if err := validateAPIKey(row.Provider, *update.APIKey); err != nil {
				return err
			}
			if row.APIKeyEnc, err = s.encryptAPIKey(*update.APIKey); err != nil {
				return err
			}
		}
		if err := tx.Save(row).Error; err != nil {
			return fmt.Errorf("update llm connection: %w", err)
		}
		out = row.connection()
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Remove deletes org's connection id and its API key. It returns
// [ErrConnectionNotFound] if there's no such connection.
func (s *Store) Remove(ctx context.Context, org syntax.DID, id ConnectionID) error {
	res := s.db.WithContext(ctx).
		Where("org_did = ? AND name = ?", org, id).
		Delete(&connectionRow{})
	if res.Error != nil {
		return fmt.Errorf("delete llm connection: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrConnectionNotFound
	}
	return nil
}

// Get returns org's connection id, or [ErrConnectionNotFound].
func (s *Store) Get(ctx context.Context, org syntax.DID, id ConnectionID) (*Connection, error) {
	row, err := getRow(s.db.WithContext(ctx), org, id)
	if err != nil {
		return nil, err
	}
	return row.connection(), nil
}

// List returns org's connections, sorted by name.
func (s *Store) List(ctx context.Context, org syntax.DID) ([]*Connection, error) {
	var rows []connectionRow
	if err := s.db.WithContext(ctx).
		Where("org_did = ?", org).
		Order("name").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list llm connections: %w", err)
	}
	conns := make([]*Connection, len(rows))
	for i := range rows {
		conns[i] = rows[i].connection()
	}
	return conns, nil
}

// Model builds the model behind org's connection id, with its API key. The
// caller must Close it. It returns [ErrConnectionNotFound] if there's no such
// connection.
func (s *Store) Model(ctx context.Context, org syntax.DID, id ConnectionID) (Model, error) {
	row, err := getRow(s.db.WithContext(ctx), org, id)
	if err != nil {
		return nil, err
	}
	apiKey, err := s.decryptAPIKey(row.APIKeyEnc)
	if err != nil {
		return nil, err
	}
	model, err := s.newModel(ctx, ModelConfig{
		Provider: row.Provider,
		Model:    row.Model,
		BaseURL:  row.BaseURL,
		APIKey:   apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("build model for llm connection %s: %w", id, err)
	}
	return model, nil
}

func getRow(db *gorm.DB, org syntax.DID, id ConnectionID) (*connectionRow, error) {
	var row connectionRow
	err := db.Where("org_did = ? AND name = ?", org, id).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrConnectionNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get llm connection: %w", err)
	}
	return &row, nil
}

// encryptAPIKey encrypts apiKey for storage, or returns "" for no key.
func (s *Store) encryptAPIKey(apiKey string) (string, error) {
	if apiKey == "" {
		return "", nil
	}
	enc, err := encrypt.EncryptCBOR(apiKey, s.key)
	if err != nil {
		return "", fmt.Errorf("encrypt llm connection api key: %w", err)
	}
	return enc, nil
}

// decryptAPIKey reverses encryptAPIKey.
func (s *Store) decryptAPIKey(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	var apiKey string
	if err := encrypt.DecryptCBOR(enc, s.key, &apiKey); err != nil {
		return "", fmt.Errorf("decrypt llm connection api key: %w", err)
	}
	return apiKey, nil
}
