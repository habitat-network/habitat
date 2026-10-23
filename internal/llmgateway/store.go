// Package llmgateway lets org admins configure LLM model connections for their
// org, and lets org members call those models through one unified API.
//
// A connection names a provider (see [Provider]), the provider's model ID and,
// optionally, a base URL. That metadata is written as a
// network.habitat.llm.connection record in the org's opensocial members space,
// keyed by the connection's name, which is also the model ID clients request.
// The provider API key never touches the record: it's stored encrypted in the
// keyval store, keyed by org and connection.
//
// Providers are reached through github.com/tmc/langchaingo, so every provider
// is called the same way (see [ModelFactory]). openai.go translates between
// that and the OpenAI Chat Completions wire format, which pear serves for each
// org.
package llmgateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_api "github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/habitat-network/habitat/internal/opensocial"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Collection is the collection connection records are written to, in the
// org's members space.
const Collection = "network.habitat.llm.connection"

// CredentialsNamespace is the keyval namespace connection API keys are stored
// under. The encrypted and plain stores handed to [NewStore] must both be
// bound to it, so the plain one can delete what the encrypted one wrote.
const CredentialsNamespace = "llm_credentials"

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
// set: they double as the record key and the model ID clients request.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func validateName(name string) error {
	if !namePattern.MatchString(name) {
		return ErrInvalidName
	}
	// "." and ".." match the pattern but aren't valid record keys.
	if _, err := syntax.ParseRecordKey(name); err != nil {
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

// Connection is an org's LLM model connection, without its API key.
type Connection struct {
	// ID is this connection's record key. It's equal to Name, chosen once at
	// creation and immutable afterward.
	ID          syntax.RecordKey
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

// credentials is the encrypted value stored per connection.
type credentials struct {
	APIKey string
}

// Store reads and writes org LLM connections: their metadata as records in the
// spaces store, and their API keys in the encrypted keyval store.
type Store struct {
	spaces   spaces.Store
	creds    keyval.EncryptedStore
	rawCreds keyval.Store
	newModel ModelFactory
}

// NewStore returns a Store writing connection records through spacesStore and
// API keys through creds. rawCreds deletes what creds wrote, so both must be
// bound to [CredentialsNamespace] over the same backing store. newModel builds
// the model a connection calls.
func NewStore(
	spacesStore spaces.Store,
	creds keyval.EncryptedStore,
	rawCreds keyval.Store,
	newModel ModelFactory,
) *Store {
	return &Store{spaces: spacesStore, creds: creds, rawCreds: rawCreds, newModel: newModel}
}

func membersSpace(org syntax.DID) habitat_syntax.SpaceURI {
	return habitat_syntax.ConstructSpaceURI(org, opensocial.MembersSpaceType, "self")
}

// credentialsKey keys a connection's API key. Org DIDs are globally unique and
// a connection's ID is unique within its org, so the pair is too.
func credentialsKey(org syntax.DID, id syntax.RecordKey) string {
	return org.String() + "/" + id.String()
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
	id := syntax.RecordKey(c.Name)
	if _, err := s.Get(ctx, org, id); err == nil {
		return nil, ErrNameTaken
	} else if !errors.Is(err, ErrConnectionNotFound) {
		return nil, err
	}

	// The key goes first, so a connection is never visible without the key it
	// was added with.
	if err := s.putAPIKey(ctx, org, id, c.APIKey); err != nil {
		return nil, err
	}
	conn := &Connection{
		ID:          id,
		Name:        c.Name,
		Description: c.Description,
		Provider:    c.Provider,
		Model:       c.Model,
		BaseURL:     c.BaseURL,
		HasAPIKey:   c.APIKey != "",
	}
	if err := s.putRecord(ctx, org, conn); err != nil {
		return nil, err
	}
	return conn, nil
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
	id syntax.RecordKey,
	update ConnectionUpdate,
) (*Connection, error) {
	conn, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	if update.Description != nil {
		conn.Description = *update.Description
	}
	if update.Model != nil {
		if strings.TrimSpace(*update.Model) == "" {
			return nil, ErrMissingModel
		}
		conn.Model = *update.Model
	}
	if update.BaseURL != nil {
		if err := validateBaseURL(conn.Provider, *update.BaseURL); err != nil {
			return nil, err
		}
		conn.BaseURL = *update.BaseURL
	}
	if update.APIKey != nil {
		if err := validateAPIKey(conn.Provider, *update.APIKey); err != nil {
			return nil, err
		}
		if err := s.putAPIKey(ctx, org, id, *update.APIKey); err != nil {
			return nil, err
		}
		conn.HasAPIKey = *update.APIKey != ""
	}
	if err := s.putRecord(ctx, org, conn); err != nil {
		return nil, err
	}
	return conn, nil
}

// Remove deletes org's connection id and its API key. It returns
// [ErrConnectionNotFound] if there's no such connection.
func (s *Store) Remove(ctx context.Context, org syntax.DID, id syntax.RecordKey) error {
	if _, err := s.Get(ctx, org, id); err != nil {
		return err
	}
	if err := s.spaces.DeleteRecord(
		ctx, membersSpace(org), org, Collection, id.String(),
	); err != nil {
		return fmt.Errorf("delete llm connection record: %w", err)
	}
	return s.putAPIKey(ctx, org, id, "")
}

// Get returns org's connection id, or [ErrConnectionNotFound].
func (s *Store) Get(ctx context.Context, org syntax.DID, id syntax.RecordKey) (*Connection, error) {
	record, err := s.spaces.GetRecord(ctx, membersSpace(org), org, Collection, id)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil, ErrConnectionNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get llm connection record: %w", err)
	}
	return s.fromRecord(ctx, org, record)
}

// List returns org's connections, sorted by name.
func (s *Store) List(ctx context.Context, org syntax.DID) ([]*Connection, error) {
	collection := syntax.NSID(Collection)
	records, err := s.spaces.ListRecords(ctx, membersSpace(org), org, &collection)
	if err != nil {
		return nil, fmt.Errorf("list llm connection records: %w", err)
	}
	conns := make([]*Connection, len(records))
	for i, record := range records {
		if conns[i], err = s.fromRecord(ctx, org, &record); err != nil {
			return nil, err
		}
	}
	slices.SortFunc(conns, func(a, b *Connection) int { return strings.Compare(a.Name, b.Name) })
	return conns, nil
}

// Model builds the model behind org's connection id, with its API key. The
// caller must Close it. It returns [ErrConnectionNotFound] if there's no such
// connection.
func (s *Store) Model(ctx context.Context, org syntax.DID, id syntax.RecordKey) (Model, error) {
	conn, err := s.Get(ctx, org, id)
	if err != nil {
		return nil, err
	}
	apiKey, err := s.apiKey(ctx, org, id)
	if err != nil {
		return nil, err
	}
	model, err := s.newModel(ctx, ModelConfig{
		Provider: conn.Provider,
		Model:    conn.Model,
		BaseURL:  conn.BaseURL,
		APIKey:   apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("build model for llm connection %s: %w", id, err)
	}
	return model, nil
}

func (s *Store) putRecord(ctx context.Context, org syntax.DID, conn *Connection) error {
	recordBytes, err := spaces.MarshalRecord(habitat_api.NetworkHabitatLlmConnection{
		Name:        conn.Name,
		Description: conn.Description,
		Provider:    string(conn.Provider),
		Model:       conn.Model,
		BaseUrl:     conn.BaseURL,
		UpdatedAt:   time.Now().Format(time.RFC3339),
	})
	if err != nil {
		return fmt.Errorf("marshal llm connection record: %w", err)
	}
	if _, _, err := s.spaces.PutRecord(
		ctx, membersSpace(org), org, Collection, conn.ID, recordBytes,
	); err != nil {
		return fmt.Errorf("put llm connection record: %w", err)
	}
	return nil
}

func (s *Store) fromRecord(
	ctx context.Context,
	org syntax.DID,
	record *spaces.Record,
) (*Connection, error) {
	// Records come back as generic maps; round-trip through JSON to type them.
	raw, err := json.Marshal(record.Value)
	if err != nil {
		return nil, fmt.Errorf("marshal llm connection record: %w", err)
	}
	var r habitat_api.NetworkHabitatLlmConnection
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil, fmt.Errorf("decode llm connection record: %w", err)
	}
	apiKey, err := s.apiKey(ctx, org, record.Rkey)
	if err != nil {
		return nil, err
	}
	return &Connection{
		ID:          record.Rkey,
		Name:        r.Name,
		Description: r.Description,
		Provider:    Provider(r.Provider),
		Model:       r.Model,
		BaseURL:     r.BaseUrl,
		HasAPIKey:   apiKey != "",
	}, nil
}

// putAPIKey stores apiKey for org's connection id, or deletes the stored one
// if apiKey is empty.
func (s *Store) putAPIKey(ctx context.Context, org syntax.DID, id syntax.RecordKey, apiKey string) error {
	key := credentialsKey(org, id)
	if apiKey == "" {
		if err := s.rawCreds.Delete(ctx, key); err != nil && !errors.Is(err, keyval.ErrNotFound) {
			return fmt.Errorf("delete llm connection api key: %w", err)
		}
		return nil
	}
	if err := s.creds.PutEncrypted(
		ctx, key, credentials{APIKey: apiKey}, keyval.NoExpiry,
	); err != nil {
		return fmt.Errorf("store llm connection api key: %w", err)
	}
	return nil
}

// apiKey returns the API key stored for org's connection id, or "" if none is.
func (s *Store) apiKey(ctx context.Context, org syntax.DID, id syntax.RecordKey) (string, error) {
	var c credentials
	err := s.creds.GetEncrypted(ctx, credentialsKey(org, id), &c)
	if errors.Is(err, keyval.ErrNotFound) {
		return "", nil
	} else if err != nil {
		return "", fmt.Errorf("get llm connection api key: %w", err)
	}
	return c.APIKey, nil
}
