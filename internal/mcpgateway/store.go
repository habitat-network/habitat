package mcpgateway

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// FlowsNamespace is the keyval namespace in-progress authorizations are
// stored under, keyed by their OAuth state, for flowTTL.
const FlowsNamespace = "mcp_oauth_flows"

// flowTTL is how long a member has to finish authorizing with a server once
// they've started.
const flowTTL = 10 * time.Minute

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

	// OAuth is the OAuth client configuration of an AuthTypeOAuth server,
	// discovered and registered when the server is added, in oauth_ columns.
	OAuth oauthConfig `gorm:"embedded;embeddedPrefix:oauth_"`
	// OAuthClientSecretEnc is OAuth's client secret encrypted with
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

// grant names did's tokens for r.
func (r *serverRow) grant(did syntax.DID) grant {
	return grant{org: r.OrgDID, server: r.Name, generation: r.Generation, did: did}
}

// flow is an in-progress authorization, as stored in keyval.
type flow struct {
	OrgID      syntax.DID
	Server     ServerName
	Generation string
	DID        syntax.DID
	Verifier   string
}

// OrgMemberships lists the orgs a user belongs to, by their members spaces.
// See opensocial.Store for the concrete implementation.
type OrgMemberships interface {
	ListMemberSpaces(ctx context.Context, user syntax.DID) ([]habitat_syntax.SpaceURI, error)
}

// Store manages org MCP server configuration and members' connections to
// those servers. It has three jobs, each its own interface.
type Store interface {
	ServerConfig
	MemberConnections
	ConnectedServers
}

// ServerConfig is an org's MCP servers, as its admins configure them.
type ServerConfig interface {
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
}

// MemberConnections is how a member connects to, and disconnects from, their
// org's servers that members reach with their own account. Each call returns
// ErrManualServer for a manual server, which has no per-member connection.
type MemberConnections interface {
	// StartAuthorization begins connecting did to orgID's OAuth server name,
	// returning the authorization URL to send did's browser to; the
	// authorization server redirects back to pear's callback, which calls
	// CompleteAuthorization. It returns ErrTokenRequired for a token server,
	// which is connected to with ConnectWithToken instead.
	StartAuthorization(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		name ServerName,
	) (authorizationURL string, err error)
	// CompleteAuthorization finishes the authorization identified by state,
	// exchanging code for the member's tokens and storing them. iss is the
	// authorization response's iss parameter, if any. It returns
	// ErrAuthorizationNotFound for an unknown, used or expired state.
	CompleteAuthorization(ctx context.Context, state, code, iss string) error
	// ConnectWithToken connects did to orgID's token server name with token,
	// did's own access token for it, which is stored encrypted. It returns
	// ErrTokenRequired for an empty token, and ErrTokenNotSupported for an
	// OAuth server, which is connected to with StartAuthorization instead.
	ConnectWithToken(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		name ServerName,
		token string,
	) error
	// DisconnectServer deletes did's tokens for orgID's OAuth or token server name, or
	// returns ErrNotConnected if there are none.
	DisconnectServer(ctx context.Context, did syntax.DID, orgID syntax.DID, name ServerName) error
}

type store struct {
	db *gorm.DB
	// vault holds members' tokens.
	vault *tokenVault
	// flows holds in-progress authorizations, keyed by their OAuth state.
	flows keyval.EncryptedStore
	// key encrypts servers' OAuth client secrets.
	key    []byte
	orgs   OrgMemberships
	client *http.Client
	// callbackURL is pear's OAuth redirect URI, where authorization servers
	// send members' browsers back to.
	callbackURL string
	// presetClients are the OAuth clients pear holds for its presets.
	presetClients map[Preset]PresetClient
}

// NewStore returns a Store keeping server configuration in db, with client
// secrets encrypted with key, and members' tokens and in-progress
// authorizations in encKV, which the store namespaces itself. client makes
// every request to MCP servers and their authorization servers, and
// callbackURL is the absolute URL pear serves its OAuth callback at.
func NewStore(
	db *gorm.DB,
	encKV keyval.EncryptedStore,
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
		vault:       newTokenVault(encKV, client),
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

// oauthConfig returns row's OAuth client configuration, with its client
// secret.
func (s *store) oauthConfig(row *serverRow) (*oauthConfig, error) {
	cfg := row.OAuth
	if row.OAuthClientSecretEnc != "" {
		if err := encrypt.DecryptCBOR(
			row.OAuthClientSecretEnc,
			s.key,
			&cfg.ClientSecret,
		); err != nil {
			return nil, fmt.Errorf("decrypt mcp server client secret: %w", err)
		}
	}
	return &cfg, nil
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
		row.OAuth = *cfg
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
	_, err := s.vault.get(ctx, row.grant(did))
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

func (s *store) StartAuthorization(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	name ServerName,
) (string, error) {
	row, err := s.getMemberRow(ctx, orgID, name)
	if err != nil {
		return "", err
	}
	if row.AuthType == AuthTypeToken {
		return "", ErrTokenRequired
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

func (s *store) CompleteAuthorization(ctx context.Context, state, code, iss string) error {
	if state == "" {
		return ErrAuthorizationNotFound
	}
	// Consumed first, so a state can't be replayed even if exchanging fails.
	var f flow
	err := s.flows.ConsumeEncrypted(ctx, state, &f)
	if errors.Is(err, keyval.ErrNotFound) {
		return ErrAuthorizationNotFound
	} else if err != nil {
		return fmt.Errorf("get mcp authorization: %w", err)
	}
	row, err := s.getRow(ctx, f.OrgID, f.Server)
	if err != nil {
		return err
	}
	if row.Generation != f.Generation {
		// Removed, and re-added, while the member was authorizing.
		return ErrServerNotFound
	}
	cfg, err := s.oauthConfig(row)
	if err != nil {
		return err
	}
	if err := cfg.checkIssuer(iss); err != nil {
		return err
	}
	if code == "" {
		return fmt.Errorf("%w: no authorization code", ErrAuthorizationFailed)
	}
	token, err := cfg.exchange(ctx, s.client, s.callbackURL, code, f.Verifier)
	if err != nil {
		return err
	}
	return s.vault.put(ctx, row.grant(f.DID), token)
}

func (s *store) ConnectWithToken(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	name ServerName,
	token string,
) error {
	row, err := s.getMemberRow(ctx, orgID, name)
	if err != nil {
		return err
	}
	if row.AuthType != AuthTypeToken {
		return ErrTokenNotSupported
	}
	if token == "" {
		return ErrTokenRequired
	}
	// A personal access token has no expiry or refresh token.
	return s.vault.put(ctx, row.grant(did), &oauth2.Token{
		AccessToken: token,
		TokenType:   "Bearer",
	})
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
	g := row.grant(did)
	if _, err := s.vault.get(ctx, g); err != nil {
		return err
	}
	return s.vault.delete(ctx, g)
}

func (s *store) ListConnectedServers(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
) ([]*Server, error) {
	memberSpaces, err := s.orgs.ListMemberSpaces(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("list member spaces: %w", err)
	}
	if !slices.ContainsFunc(memberSpaces, func(space habitat_syntax.SpaceURI) bool {
		return space.SpaceOwner() == orgID
	}) {
		return nil, nil
	}
	rows, err := s.listRows(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var out []*Server
	for i := range rows {
		connected, err := s.connected(ctx, &rows[i], did)
		if err != nil {
			return nil, err
		}
		if connected {
			out = append(out, rows[i].server())
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
	g := row.grant(did)
	token, err := s.vault.get(ctx, g)
	if err != nil {
		return nil, err
	}
	if token.needsRefresh() {
		cfg, err := s.oauthConfig(row)
		if err != nil {
			return nil, err
		}
		if token, err = s.vault.refresh(ctx, g, cfg); err != nil {
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
