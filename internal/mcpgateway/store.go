package mcpgateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/oauth2"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// TokensNamespace is the keyval namespace each member's OAuth tokens for an
// org's MCP servers are stored under, keyed by tokenKey.
const TokensNamespace = "mcp_oauth_tokens"

// FlowsNamespace is the keyval namespace in-progress authorizations are
// stored under, keyed by their OAuth state, for flowTTL.
const FlowsNamespace = "mcp_oauth_flows"

// flowTTL is how long a member has to finish authorizing with a server once
// they've started.
const flowTTL = 10 * time.Minute

// expirySkew is how long before its expiry an access token is refreshed, so
// it doesn't expire in flight.
const expirySkew = time.Minute

// refreshTimeout bounds a token refresh, which outlives the request that
// started it (see store.refresh).
const refreshTimeout = 30 * time.Second

// serverRow is the GORM model for an org's MCP server.
type serverRow struct {
	OrgDID      syntax.DID `gorm:"column:org_did;primaryKey;size:255"`
	Name        ServerName `gorm:"column:name;primaryKey;size:64"`
	Description string     `gorm:"column:description;not null;default:''"`
	URL         string     `gorm:"column:url;not null"`
	AuthType    AuthType   `gorm:"column:auth_type;not null"`
	// Generation is random, and new each time a server is added. Members'
	// tokens are bound to it, so a removed server's tokens, which keyval
	// can't enumerate to delete, never reach one re-added under its name.
	Generation string `gorm:"column:generation;not null"`

	// The OAuth client configuration of AuthTypeOAuth servers (see
	// oauthConfig), discovered and registered when the server is added.
	OAuthResource              string           `gorm:"column:oauth_resource;not null;default:''"`
	OAuthIssuer                string           `gorm:"column:oauth_issuer;not null;default:''"`
	OAuthAuthURL               string           `gorm:"column:oauth_auth_url;not null;default:''"`
	OAuthTokenURL              string           `gorm:"column:oauth_token_url;not null;default:''"`
	OAuthIssParameterSupported bool             `gorm:"column:oauth_iss_parameter_supported;not null;default:false"`
	OAuthScopes                string           `gorm:"column:oauth_scopes;not null;default:''"`
	OAuthClientID              string           `gorm:"column:oauth_client_id;not null;default:''"`
	OAuthAuthStyle             oauth2.AuthStyle `gorm:"column:oauth_auth_style;not null;default:0"`
	// OAuthClientSecretEnc is the client secret encrypted with
	// encrypt.EncryptCBOR, or empty for a public client.
	OAuthClientSecretEnc string `gorm:"column:oauth_client_secret_enc;not null;default:''"`

	CreatedAt time.Time `gorm:"column:created_at;not null"`
	UpdatedAt time.Time `gorm:"column:updated_at;not null"`
}

func (serverRow) TableName() string { return "mcp_servers" }

// Models returns the GORM models this package persists, for db.Migrate.
func Models() []any { return []any{&serverRow{}} }

func (r *serverRow) server() *Server {
	return &Server{
		OrgID:       r.OrgDID,
		Name:        r.Name,
		Description: r.Description,
		AuthType:    r.AuthType,
		URL:         r.URL,
	}
}

// storedToken is a member's OAuth tokens for a server, as stored in keyval.
type storedToken struct {
	// Generation is the serverRow.Generation the tokens were issued for.
	Generation   string
	AccessToken  string
	TokenType    string
	RefreshToken string
	Expiry       time.Time
}

func newStoredToken(generation string, token *oauth2.Token) *storedToken {
	return &storedToken{
		Generation:   generation,
		AccessToken:  token.AccessToken,
		TokenType:    token.TokenType,
		RefreshToken: token.RefreshToken,
		Expiry:       token.Expiry,
	}
}

// needsRefresh reports whether t's access token expires within expirySkew.
// Tokens without an expiry, such as personal access tokens, never do.
func (t *storedToken) needsRefresh() bool {
	return !t.Expiry.IsZero() && time.Until(t.Expiry) < expirySkew
}

func (t *storedToken) oauth2() *oauth2.Token {
	return &oauth2.Token{
		AccessToken:  t.AccessToken,
		TokenType:    t.TokenType,
		RefreshToken: t.RefreshToken,
		Expiry:       t.Expiry,
	}
}

// flow is an in-progress authorization, as stored in keyval.
type flow struct {
	OrgID      syntax.DID
	Server     ServerName
	Generation string
	DID        syntax.DID
	Verifier   string
}

// tokenKey keys did's tokens for orgID's server name. Org DIDs are globally
// unique and a server's name is unique within its org, so the triple is too.
func tokenKey(orgID syntax.DID, name ServerName, did syntax.DID) string {
	return orgID.String() + "|" + string(name) + "|" + did.String()
}

// OrgMemberships lists the orgs a user belongs to, by their members spaces.
// See opensocial.Store for the concrete implementation.
type OrgMemberships interface {
	ListMemberSpaces(ctx context.Context, user syntax.DID) ([]habitat_syntax.SpaceURI, error)
}

// CompletedAuthorization is the server a member just connected to.
type CompletedAuthorization struct {
	OrgID  syntax.DID
	Server ServerName
	DID    syntax.DID
}

// Store manages org MCP server configuration and members' connections to
// those servers.
type Store interface {
	// AddServer adds an MCP server to orgID. For an OAuth server it first
	// discovers the server's authorization server and registers pear as a
	// client of it; nobody signs in yet.
	AddServer(ctx context.Context, orgID syntax.DID, server NewServer) (*Server, error)
	// UpdateServer updates an existing org MCP server. It returns
	// ErrServerNotFound if there's no such server.
	UpdateServer(
		ctx context.Context,
		orgID syntax.DID,
		name ServerName,
		update ServerUpdate,
	) (*Server, error)
	// RemoveServer deletes an org's MCP server. Members' tokens for it are
	// left behind but never used again (see serverRow.Generation).
	RemoveServer(ctx context.Context, orgID syntax.DID, name ServerName) error
	// ListServers lists the MCP servers configured for an org, sorted by
	// name, along with whether did has connected to each one.
	ListServers(ctx context.Context, orgID syntax.DID, did syntax.DID) ([]*ServerWithStatus, error)

	// StartAuthorization begins connecting did to orgID's server name. For
	// an OAuth server it returns the authorization URL to send did's browser
	// to (token must be empty); the authorization server redirects back to
	// pear's callback, which calls CompleteAuthorization. For a token server
	// it stores token as did's access token, which connects did right away,
	// and returns no URL; the token is required.
	StartAuthorization(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		name ServerName,
		token string,
	) (authorizationURL string, err error)
	// CompleteAuthorization finishes the authorization identified by state,
	// exchanging code for did's tokens and storing them. iss is the
	// authorization response's iss parameter, if any. It returns
	// ErrAuthorizationNotFound for an unknown, used or expired state.
	CompleteAuthorization(
		ctx context.Context,
		state, code, iss string,
	) (*CompletedAuthorization, error)
	// DisconnectServer deletes did's tokens for orgID's OAuth or token server name, or
	// returns ErrNotConnected if there are none.
	DisconnectServer(ctx context.Context, did syntax.DID, orgID syntax.DID, name ServerName) error

	// ListConnectedServers lists the servers did can reach: every manual
	// server, and every OAuth server did has connected to, of every org did
	// belongs to.
	ListConnectedServers(ctx context.Context, did syntax.DID) ([]*Server, error)
	// Downstream returns how to reach orgID's server name on did's behalf,
	// refreshing did's access token if it's about to expire. It returns
	// ErrNotConnected if did isn't connected to it.
	Downstream(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		name ServerName,
	) (*Downstream, error)
}

type store struct {
	db        *gorm.DB
	tokens    keyval.EncryptedStore
	rawTokens keyval.Store
	flows     keyval.EncryptedStore
	key       []byte
	orgs      OrgMemberships
	client    *http.Client
	// callbackURL is pear's OAuth redirect URI, where authorization servers
	// send members' browsers back to.
	callbackURL string
	// presetClients are the OAuth clients pear holds for its presets.
	presetClients map[Preset]PresetClient
	// refreshes collapses concurrent refreshes of the same tokens within this
	// process, since a rotating refresh token can only be redeemed once.
	refreshes singleflight.Group
}

// NewStore returns a Store keeping server configuration in db, with client
// secrets encrypted with key, and members' tokens and in-progress
// authorizations in encKV, deleting through kv. encKV and kv must be the same
// backing store; the store namespaces both itself. client makes every request
// to MCP servers and their authorization servers, and callbackURL is the
// absolute URL pear serves its OAuth callback at.
func NewStore(
	db *gorm.DB,
	encKV keyval.EncryptedStore,
	kv keyval.Store,
	key []byte,
	orgs OrgMemberships,
	client *http.Client,
	callbackURL string,
	opts ...Option,
) (Store, error) {
	if _, err := encrypt.EncryptCBOR(nil, key); err != nil {
		return nil, fmt.Errorf("mcpgateway: invalid encryption key: %w", err)
	}
	if orgs == nil {
		return nil, errors.New("mcpgateway: org memberships are required")
	}
	if client == nil {
		return nil, errors.New("mcpgateway: http client is required")
	}
	if err := validateServerURL(callbackURL); err != nil {
		return nil, fmt.Errorf("mcpgateway: callback url: %w", err)
	}
	s := &store{
		db:          db,
		tokens:      encKV.Namespace(TokensNamespace),
		rawTokens:   kv.Namespace(TokensNamespace),
		flows:       encKV.Namespace(FlowsNamespace),
		key:         key,
		orgs:        orgs,
		client:      client,
		callbackURL: callbackURL,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func (s *store) getRow(ctx context.Context, orgID syntax.DID, name ServerName) (*serverRow, error) {
	var row serverRow
	err := s.db.WithContext(ctx).
		Where("org_did = ? AND name = ?", orgID, name).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrServerNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get mcp server: %w", err)
	}
	return &row, nil
}

// getOAuthRow returns orgID's OAuth server name, or ErrManualServer if it's a
// manual server.
func (s *store) getOAuthRow(
	ctx context.Context,
	orgID syntax.DID,
	name ServerName,
) (*serverRow, error) {
	row, err := s.getRow(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if row.AuthType != AuthTypeOAuth {
		return nil, ErrManualServer
	}
	return row, nil
}

// getMemberRow returns orgID's server name that members connect to
// individually (oauth or token), or ErrManualServer if it's a manual server.
func (s *store) getMemberRow(
	ctx context.Context,
	orgID syntax.DID,
	name ServerName,
) (*serverRow, error) {
	row, err := s.getRow(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if row.AuthType == AuthTypeManual {
		return nil, ErrManualServer
	}
	return row, nil
}

func (s *store) oauthConfig(row *serverRow) (*oauthConfig, error) {
	cfg := &oauthConfig{
		Resource:              row.OAuthResource,
		Issuer:                row.OAuthIssuer,
		AuthURL:               row.OAuthAuthURL,
		TokenURL:              row.OAuthTokenURL,
		IssParameterSupported: row.OAuthIssParameterSupported,
		Scopes:                strings.Fields(row.OAuthScopes),
		ClientID:              row.OAuthClientID,
		AuthStyle:             row.OAuthAuthStyle,
	}
	if row.OAuthClientSecretEnc != "" {
		if err := encrypt.DecryptCBOR(
			row.OAuthClientSecretEnc,
			s.key,
			&cfg.ClientSecret,
		); err != nil {
			return nil, fmt.Errorf("decrypt mcp server client secret: %w", err)
		}
	}
	return cfg, nil
}

func (s *store) AddServer(ctx context.Context, orgID syntax.DID, ns NewServer) (*Server, error) {
	if err := s.applyPreset(&ns); err != nil {
		return nil, err
	}
	if err := validateServerName(ns.Name); err != nil {
		return nil, err
	}
	if err := validateServerURL(ns.URL); err != nil {
		return nil, err
	}
	if ns.AuthType != AuthTypeOAuth && ns.AuthType != AuthTypeManual &&
		ns.AuthType != AuthTypeToken {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAuthType, ns.AuthType)
	}
	if ns.ClientSecret != "" && ns.ClientID == "" {
		return nil, ErrClientSecretWithoutID
	}
	if ns.ClientID != "" && ns.AuthType != AuthTypeOAuth {
		return nil, ErrClientNotSupported
	}
	name := ServerName(ns.Name)
	// Checked up front, before registering a client nobody would use.
	if _, err := s.getRow(ctx, orgID, name); err == nil {
		return nil, fmt.Errorf("%w: %q", ErrServerNameTaken, name)
	} else if !errors.Is(err, ErrServerNotFound) {
		return nil, err
	}

	row := &serverRow{
		OrgDID:      orgID,
		Name:        name,
		Description: ns.Description,
		URL:         ns.URL,
		AuthType:    ns.AuthType,
		Generation:  rand.Text(),
	}
	if ns.AuthType == AuthTypeOAuth {
		var preregistered *preregisteredClient
		if ns.ClientID != "" {
			preregistered = &preregisteredClient{ID: ns.ClientID, Secret: ns.ClientSecret}
		}
		cfg, err := discoverOAuth(ctx, s.client, ns.URL, s.callbackURL, preregistered)
		if err != nil {
			return nil, err
		}
		row.OAuthResource = cfg.Resource
		row.OAuthIssuer = cfg.Issuer
		row.OAuthAuthURL = cfg.AuthURL
		row.OAuthTokenURL = cfg.TokenURL
		row.OAuthIssParameterSupported = cfg.IssParameterSupported
		row.OAuthScopes = strings.Join(cfg.Scopes, " ")
		row.OAuthClientID = cfg.ClientID
		row.OAuthAuthStyle = cfg.AuthStyle
		if cfg.ClientSecret != "" {
			if row.OAuthClientSecretEnc, err = encrypt.EncryptCBOR(
				cfg.ClientSecret,
				s.key,
			); err != nil {
				return nil, fmt.Errorf("encrypt mcp server client secret: %w", err)
			}
		}
	}
	err := s.db.WithContext(ctx).Create(row).Error
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return nil, fmt.Errorf("%w: %q", ErrServerNameTaken, name)
	} else if err != nil {
		return nil, fmt.Errorf("insert mcp server: %w", err)
	}
	return row.server(), nil
}

func (s *store) UpdateServer(
	ctx context.Context,
	orgID syntax.DID,
	name ServerName,
	update ServerUpdate,
) (*Server, error) {
	row, err := s.getRow(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if update.Description != nil {
		row.Description = *update.Description
	}
	if update.URL != nil && *update.URL != row.URL {
		if row.AuthType != AuthTypeManual {
			return nil, ErrURLImmutable
		}
		if err := validateServerURL(*update.URL); err != nil {
			return nil, err
		}
		row.URL = *update.URL
	}
	if err := s.db.WithContext(ctx).Save(row).Error; err != nil {
		return nil, fmt.Errorf("update mcp server: %w", err)
	}
	return row.server(), nil
}

func (s *store) RemoveServer(ctx context.Context, orgID syntax.DID, name ServerName) error {
	res := s.db.WithContext(ctx).
		Where("org_did = ? AND name = ?", orgID, name).
		Delete(&serverRow{})
	if res.Error != nil {
		return fmt.Errorf("delete mcp server: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrServerNotFound
	}
	return nil
}

func (s *store) listRows(ctx context.Context, orgID syntax.DID) ([]serverRow, error) {
	var rows []serverRow
	if err := s.db.WithContext(ctx).
		Where("org_did = ?", orgID).
		Order("name").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list mcp servers: %w", err)
	}
	return rows, nil
}

// connected reports whether did can reach row: always for a manual server,
// and for an OAuth server if did holds tokens issued for it.
func (s *store) connected(ctx context.Context, row *serverRow, did syntax.DID) (bool, error) {
	if row.AuthType == AuthTypeManual {
		return true, nil
	}
	_, err := s.token(ctx, row, did)
	if errors.Is(err, ErrNotConnected) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	return true, nil
}

func (s *store) ListServers(
	ctx context.Context,
	orgID syntax.DID,
	did syntax.DID,
) ([]*ServerWithStatus, error) {
	rows, err := s.listRows(ctx, orgID)
	if err != nil {
		return nil, err
	}
	out := make([]*ServerWithStatus, len(rows))
	for i := range rows {
		connected, err := s.connected(ctx, &rows[i], did)
		if err != nil {
			return nil, err
		}
		out[i] = &ServerWithStatus{Server: rows[i].server(), Connected: connected}
	}
	return out, nil
}

// token returns did's tokens for row, or ErrNotConnected if did has none
// issued for it.
func (s *store) token(ctx context.Context, row *serverRow, did syntax.DID) (*storedToken, error) {
	var t storedToken
	err := s.tokens.GetEncrypted(ctx, tokenKey(row.OrgDID, row.Name, did), &t)
	if errors.Is(err, keyval.ErrNotFound) {
		return nil, ErrNotConnected
	} else if err != nil {
		return nil, fmt.Errorf("get mcp server tokens: %w", err)
	}
	if t.Generation != row.Generation {
		return nil, ErrNotConnected
	}
	return &t, nil
}

func (s *store) putToken(
	ctx context.Context,
	row *serverRow,
	did syntax.DID,
	token *oauth2.Token,
) error {
	if err := s.tokens.PutEncrypted(
		ctx, tokenKey(row.OrgDID, row.Name, did), newStoredToken(row.Generation, token),
		keyval.NoExpiry,
	); err != nil {
		return fmt.Errorf("store mcp server tokens: %w", err)
	}
	return nil
}

func (s *store) deleteToken(
	ctx context.Context,
	orgID syntax.DID,
	name ServerName,
	did syntax.DID,
) error {
	err := s.rawTokens.Delete(ctx, tokenKey(orgID, name, did))
	if err != nil && !errors.Is(err, keyval.ErrNotFound) {
		return fmt.Errorf("delete mcp server tokens: %w", err)
	}
	return nil
}

func (s *store) StartAuthorization(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	name ServerName,
	token string,
) (string, error) {
	row, err := s.getMemberRow(ctx, orgID, name)
	if err != nil {
		return "", err
	}
	if row.AuthType == AuthTypeToken {
		if token == "" {
			return "", ErrTokenRequired
		}
		// A personal access token has no expiry or refresh token.
		return "", s.putToken(ctx, row, did, &oauth2.Token{
			AccessToken: token,
			TokenType:   "Bearer",
		})
	}
	if token != "" {
		return "", ErrTokenNotSupported
	}
	cfg, err := s.oauthConfig(row)
	if err != nil {
		return "", err
	}
	state := rand.Text()
	verifier := oauth2.GenerateVerifier()
	if err := s.flows.PutEncrypted(ctx, state, flow{
		OrgID:      orgID,
		Server:     name,
		Generation: row.Generation,
		DID:        did,
		Verifier:   verifier,
	}, flowTTL); err != nil {
		return "", fmt.Errorf("store mcp authorization: %w", err)
	}
	return cfg.authorizationURL(s.callbackURL, state, verifier), nil
}

func (s *store) CompleteAuthorization(
	ctx context.Context,
	state, code, iss string,
) (*CompletedAuthorization, error) {
	if state == "" {
		return nil, ErrAuthorizationNotFound
	}
	// Consumed first, so a state can't be replayed even if exchanging fails.
	var f flow
	err := s.flows.ConsumeEncrypted(ctx, state, &f)
	if errors.Is(err, keyval.ErrNotFound) {
		return nil, ErrAuthorizationNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get mcp authorization: %w", err)
	}
	completed := &CompletedAuthorization{OrgID: f.OrgID, Server: f.Server, DID: f.DID}
	row, err := s.getOAuthRow(ctx, f.OrgID, f.Server)
	if errors.Is(err, ErrServerNotFound) || (err == nil && row.Generation != f.Generation) {
		// Removed, and maybe re-added, while the member was authorizing.
		return completed, ErrServerNotFound
	} else if err != nil {
		return completed, err
	}
	cfg, err := s.oauthConfig(row)
	if err != nil {
		return completed, err
	}
	if err := cfg.checkIssuer(iss); err != nil {
		return completed, err
	}
	if code == "" {
		return completed, fmt.Errorf("%w: no authorization code", ErrAuthorizationFailed)
	}
	token, err := cfg.exchange(ctx, s.client, s.callbackURL, code, f.Verifier)
	if err != nil {
		return completed, err
	}
	if err := s.putToken(ctx, row, f.DID, token); err != nil {
		return completed, err
	}
	return completed, nil
}

func (s *store) DisconnectServer(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	name ServerName,
) error {
	row, err := s.getMemberRow(ctx, orgID, name)
	if err != nil {
		return err
	}
	if _, err := s.token(ctx, row, did); err != nil {
		return err
	}
	return s.deleteToken(ctx, orgID, name, did)
}

func (s *store) ListConnectedServers(ctx context.Context, did syntax.DID) ([]*Server, error) {
	memberSpaces, err := s.orgs.ListMemberSpaces(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("list member spaces: %w", err)
	}
	var out []*Server
	for _, space := range memberSpaces {
		rows, err := s.listRows(ctx, space.SpaceOwner())
		if err != nil {
			return nil, err
		}
		for i := range rows {
			connected, err := s.connected(ctx, &rows[i], did)
			if err != nil {
				return nil, err
			}
			if connected {
				out = append(out, rows[i].server())
			}
		}
	}
	return out, nil
}

func (s *store) Downstream(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	name ServerName,
) (*Downstream, error) {
	row, err := s.getRow(ctx, orgID, name)
	if err != nil {
		return nil, err
	}
	if row.AuthType == AuthTypeManual {
		return &Downstream{URL: row.URL, Client: s.client}, nil
	}
	token, err := s.token(ctx, row, did)
	if err != nil {
		return nil, err
	}
	if token.needsRefresh() {
		if token, err = s.refresh(ctx, row, did); err != nil {
			return nil, err
		}
	}
	return &Downstream{
		URL:    row.URL,
		Client: s.client,
		Headers: map[string]string{
			"Authorization": token.oauth2().Type() + " " + token.AccessToken,
		},
	}, nil
}

// refresh redeems did's refresh token for new tokens and stores them,
// before any caller uses them: a rotated refresh token that isn't stored would
// leave did disconnected at the next refresh. If the authorization server no
// longer honors the refresh token, did's tokens are deleted and refresh
// returns ErrNotConnected.
//
// Concurrent refreshes within this process share one request. Refreshes on
// other pear instances aren't coordinated, so with a server that rotates
// refresh tokens, a race between two instances can disconnect did.
func (s *store) refresh(
	ctx context.Context,
	row *serverRow,
	did syntax.DID,
) (*storedToken, error) {
	key := tokenKey(row.OrgDID, row.Name, did)
	// The refresh is shared by every caller waiting on it, and once the
	// authorization server has rotated the refresh token the new one must be
	// stored, so it isn't cut short when the caller that started it goes away.
	// refreshTimeout bounds it instead.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	v, err, _ := s.refreshes.Do(key, func() (any, error) {
		// Read the tokens again, rather than using the caller's copy: another
		// caller may have refreshed them since, rotating the refresh token the
		// caller read.
		current, err := s.token(ctx, row, did)
		if err != nil {
			return nil, err
		}
		if !current.needsRefresh() {
			return current, nil
		}
		if current.RefreshToken == "" {
			return nil, fmt.Errorf("%w: access token expired", ErrNotConnected)
		}
		cfg, err := s.oauthConfig(row)
		if err != nil {
			return nil, err
		}
		refreshed, err := cfg.refresh(ctx, s.client, current.RefreshToken)
		if errors.Is(err, errInvalidGrant) {
			slog.InfoContext(ctx, "mcp: refresh token rejected; disconnecting",
				"org", row.OrgDID.String(), "server", string(row.Name), "did", did.String())
			if err := s.deleteToken(ctx, row.OrgDID, row.Name, did); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("%w: sign-in expired", ErrNotConnected)
		} else if err != nil {
			return nil, err
		}
		if err := s.putToken(ctx, row, did, refreshed); err != nil {
			return nil, err
		}
		return newStoredToken(row.Generation, refreshed), nil
	})
	if err != nil {
		return nil, err
	}
	refreshed, _ := v.(*storedToken)
	return refreshed, nil
}
