// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members opt in to authorizing with
// those servers via OAuth per the MCP authorization spec
// (https://modelcontextprotocol.io/docs/tutorials/security/authorization).
package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/habitat-network/habitat/internal/encrypt"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
)

var (
	// ErrServerNotFound is returned when no MCP server matches the given ID/org.
	ErrServerNotFound = errors.New("mcp server not found")
	// ErrCredentialNotFound is returned when a user has no stored credential for a server.
	ErrCredentialNotFound = errors.New("mcp server credential not found")
	// ErrNotOAuthServer is returned when starting authorization against a server that
	// doesn't require it.
	ErrNotOAuthServer = errors.New("mcp server does not require oauth authorization")
	// ErrPendingAuthorizationNotFound is returned when completing an authorization
	// flow whose state doesn't match (or has already been completed).
	ErrPendingAuthorizationNotFound = errors.New("pending mcp authorization not found")
)

// Store manages MCP server configuration and per-user OAuth credentials for orgs.
type Store interface {
	// AddServer configures a new MCP server for the org, probing it to detect
	// whether it requires OAuth authorization and, if so, discovering and
	// registering with its authorization server.
	AddServer(ctx context.Context, orgID syntax.DID, name, url, description string) (*Server, error)
	// UpdateServer updates fields of an existing org MCP server. A nil field is
	// left unchanged. If url changes, the server is re-probed as in AddServer.
	UpdateServer(
		ctx context.Context,
		orgID syntax.DID,
		id ServerID,
		name, url, description *string,
	) (*Server, error)
	// RemoveServer deletes an org's MCP server along with any stored user credentials for it.
	RemoveServer(ctx context.Context, orgID syntax.DID, id ServerID) error
	// ListServers lists the MCP servers configured for an org.
	ListServers(ctx context.Context, orgID syntax.DID) ([]*Server, error)
	// GetServer fetches a single MCP server by ID, scoped to the org.
	GetServer(ctx context.Context, orgID syntax.DID, id ServerID) (*Server, error)

	// StartAuthorization begins an OAuth authorization-code flow for did to
	// connect to an org's MCP server, returning the URL to redirect the
	// caller's browser to. returnURL is where the browser is sent once
	// authorization completes; it must be an HTTPS URL (or a loopback
	// address, for local development).
	StartAuthorization(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		id ServerID,
		returnURL string,
	) (authorizationURL string, err error)
	// CompleteAuthorization finishes an in-flight flow started by
	// StartAuthorization: it exchanges code for tokens and stores them,
	// returning who the flow was for and the returnURL passed to
	// StartAuthorization.
	CompleteAuthorization(
		ctx context.Context,
		state, code string,
	) (did syntax.DID, orgID syntax.DID, id ServerID, returnURL string, err error)

	// DisconnectServer removes a user's stored credential for an MCP server.
	DisconnectServer(ctx context.Context, did syntax.DID, id ServerID) error
	// IsConnected reports whether a user has a stored credential for an MCP server.
	IsConnected(ctx context.Context, did syntax.DID, id ServerID) (bool, error)
	// GetAccessToken returns a valid access token for did to call an MCP
	// server with, transparently refreshing it if it has expired.
	GetAccessToken(ctx context.Context, did syntax.DID, id ServerID) (string, error)
}

type store struct {
	db            *gorm.DB
	encryptionKey []byte
	httpClient    *http.Client
	// redirectURL is this habitat instance's own OAuth callback URL, used as
	// the redirect_uri when registering with and authorizing against MCP
	// servers' authorization servers.
	redirectURL string
}

// NewStore constructs a Store backed by db, encrypting stored user credentials
// with encryptionKey. httpClient is used for MCP server/authorization-server
// discovery and token requests; redirectURL is this instance's OAuth callback URL.
func NewStore(
	db *gorm.DB,
	encryptionKey []byte,
	httpClient *http.Client,
	redirectURL string,
) (Store, error) {
	if encryptionKey == nil {
		return nil, fmt.Errorf("encryption key is required")
	}
	if redirectURL == "" {
		return nil, fmt.Errorf("redirect url is required")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	if err := db.AutoMigrate(&serverModel{}, &credentialModel{}, &pendingAuthModel{}); err != nil {
		return nil, fmt.Errorf("failed to migrate database: %w", err)
	}
	return &store{
		db:            db,
		encryptionKey: encryptionKey,
		httpClient:    httpClient,
		redirectURL:   redirectURL,
	}, nil
}

func (s *store) encrypt(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	return encrypt.EncryptCBOR(plaintext, s.encryptionKey)
}

func (s *store) decrypt(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	var plaintext string
	if err := encrypt.DecryptCBOR(ciphertext, s.encryptionKey, &plaintext); err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return plaintext, nil
}

func (s *store) applyOAuthConfig(m *serverModel, cfg *oauthConfig) error {
	if cfg == nil {
		m.OAuthIssuer = ""
		m.OAuthAuthorizationEndpoint = ""
		m.OAuthTokenEndpoint = ""
		m.OAuthScopes = ""
		m.OAuthClientID = ""
		m.OAuthClientSecret = ""
		return nil
	}
	m.OAuthIssuer = cfg.Issuer
	m.OAuthAuthorizationEndpoint = cfg.AuthorizationEndpoint
	m.OAuthTokenEndpoint = cfg.TokenEndpoint
	m.OAuthScopes = strings.Join(cfg.Scopes, " ")
	m.OAuthClientID = cfg.ClientID
	encryptedSecret, err := s.encrypt(cfg.ClientSecret)
	if err != nil {
		return fmt.Errorf("encrypt oauth client secret: %w", err)
	}
	m.OAuthClientSecret = encryptedSecret
	return nil
}

func (s *store) oauth2Config(m *serverModel) (*oauth2.Config, error) {
	clientSecret, err := s.decrypt(m.OAuthClientSecret)
	if err != nil {
		return nil, fmt.Errorf("decrypt oauth client secret: %w", err)
	}
	cfg := &oauth2.Config{
		ClientID:     m.OAuthClientID,
		ClientSecret: clientSecret,
		Endpoint: oauth2.Endpoint{
			AuthURL:  m.OAuthAuthorizationEndpoint,
			TokenURL: m.OAuthTokenEndpoint,
		},
		RedirectURL: s.redirectURL,
	}
	if m.OAuthScopes != "" {
		cfg.Scopes = strings.Fields(m.OAuthScopes)
	}
	return cfg, nil
}

func (s *store) AddServer(
	ctx context.Context,
	orgID syntax.DID,
	name, url, description string,
) (*Server, error) {
	authType, cfg, err := s.detectAuth(ctx, url)
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
	if err := s.applyOAuthConfig(&m, cfg); err != nil {
		return nil, err
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

func (s *store) getServerModelByID(ctx context.Context, id ServerID) (*serverModel, error) {
	var m serverModel
	err := s.db.WithContext(ctx).Where("id = ?", id).First(&m).Error
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
		authType, cfg, err := s.detectAuth(ctx, *url)
		if err != nil {
			return nil, err
		}
		m.URL = *url
		m.AuthType = authType
		if err := s.applyOAuthConfig(m, cfg); err != nil {
			return nil, err
		}
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
		if err := tx.Where("server_id = ?", id).Delete(&pendingAuthModel{}).Error; err != nil {
			return fmt.Errorf("failed to remove pending mcp authorizations: %w", err)
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

func (s *store) StartAuthorization(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	id ServerID,
	returnURL string,
) (string, error) {
	if err := validateReturnURL(returnURL); err != nil {
		return "", err
	}

	m, err := s.getServerModel(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	if m.AuthType != AuthTypeOAuth {
		return "", ErrNotOAuthServer
	}

	cfg, err := s.oauth2Config(m)
	if err != nil {
		return "", err
	}

	verifier := oauth2.GenerateVerifier()
	state := uuid.NewString()
	pending := pendingAuthModel{
		State:        state,
		ServerID:     id,
		DID:          did,
		OrgID:        orgID,
		CodeVerifier: verifier,
		ReturnURL:    returnURL,
	}
	if err := s.db.WithContext(ctx).Create(&pending).Error; err != nil {
		return "", fmt.Errorf("failed to create pending mcp authorization: %w", err)
	}

	authURL := cfg.AuthCodeURL(
		state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("resource", m.URL),
	)
	return authURL, nil
}

// validateReturnURL guards against StartAuthorization's caller-supplied
// returnURL being used for an open redirect: it must be an absolute HTTPS
// URL, or a loopback address for local development.
func validateReturnURL(returnURL string) error {
	u, err := url.Parse(returnURL)
	if err != nil {
		return fmt.Errorf("invalid return url: %w", err)
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		return fmt.Errorf("return url must be HTTPS (or a loopback address for local development)")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	return net.ParseIP(host).IsLoopback()
}

func (s *store) CompleteAuthorization(
	ctx context.Context,
	state, code string,
) (syntax.DID, syntax.DID, ServerID, string, error) {
	var pending pendingAuthModel
	err := s.db.WithContext(ctx).Where("state = ?", state).First(&pending).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", "", "", "", ErrPendingAuthorizationNotFound
	} else if err != nil {
		return "", "", "", "", fmt.Errorf("failed to get pending mcp authorization: %w", err)
	}

	m, err := s.getServerModel(ctx, pending.OrgID, pending.ServerID)
	if err != nil {
		return "", "", "", "", err
	}
	cfg, err := s.oauth2Config(m)
	if err != nil {
		return "", "", "", "", err
	}

	token, err := cfg.Exchange(ctx, code, oauth2.VerifierOption(pending.CodeVerifier))
	if err != nil {
		return "", "", "", "", fmt.Errorf("exchange authorization code: %w", err)
	}

	if err := s.saveToken(ctx, pending.DID, pending.ServerID, token); err != nil {
		return "", "", "", "", err
	}

	if err := s.db.WithContext(ctx).Delete(&pendingAuthModel{}, "state = ?", state).Error; err != nil {
		return "", "", "", "", fmt.Errorf("failed to clean up pending mcp authorization: %w", err)
	}

	return pending.DID, pending.OrgID, pending.ServerID, pending.ReturnURL, nil
}

func (s *store) saveToken(
	ctx context.Context,
	did syntax.DID,
	id ServerID,
	token *oauth2.Token,
) error {
	cred := credentialModel{
		ServerID:  id,
		DID:       did,
		TokenType: token.TokenType,
		Expiry:    token.Expiry,
	}
	var err error
	if cred.AccessToken, err = s.encrypt(token.AccessToken); err != nil {
		return fmt.Errorf("encrypt access token: %w", err)
	}
	if cred.RefreshToken, err = s.encrypt(token.RefreshToken); err != nil {
		return fmt.Errorf("encrypt refresh token: %w", err)
	}
	if err := s.db.WithContext(ctx).Save(&cred).Error; err != nil {
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
		return nil, fmt.Errorf("failed to get mcp server credential: %w", err)
	}
	return &cred, nil
}

func (s *store) GetAccessToken(ctx context.Context, did syntax.DID, id ServerID) (string, error) {
	cred, err := s.getCredentialModel(ctx, did, id)
	if err != nil {
		return "", err
	}
	accessToken, err := s.decrypt(cred.AccessToken)
	if err != nil {
		return "", err
	}
	if cred.Expiry.IsZero() || time.Now().Before(cred.Expiry) {
		return accessToken, nil
	}

	refreshToken, err := s.decrypt(cred.RefreshToken)
	if err != nil {
		return "", err
	}
	if refreshToken == "" {
		return "", fmt.Errorf("mcp access token expired and no refresh token is stored")
	}

	m, err := s.getServerModelByID(ctx, id)
	if err != nil {
		return "", err
	}
	cfg, err := s.oauth2Config(m)
	if err != nil {
		return "", err
	}
	tokenSource := cfg.TokenSource(ctx, &oauth2.Token{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		Expiry:       cred.Expiry,
		TokenType:    cred.TokenType,
	})
	refreshed, err := tokenSource.Token()
	if err != nil {
		return "", fmt.Errorf("refresh mcp access token: %w", err)
	}
	if refreshed.AccessToken != accessToken {
		if err := s.saveToken(ctx, did, id, refreshed); err != nil {
			return "", err
		}
	}
	return refreshed.AccessToken, nil
}
