// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members opt in to authorizing with
// those servers. An admin adds a server by entering its name, description,
// URL and AuthType, which are written as a network.habitat.mcp.server record
// in the org's opensocial members space (see internal/opensocial). Nobody
// signs in while adding it.
//
//   - AuthTypeOAuth: each member later connects with StartAuthorization,
//     which is brokered entirely through Nango
//     (https://nango.dev/docs/guides/auth/mcp-auth) via its mcp-generic
//     connector with the server's URL pre-filled. The connector
//     discovers/registers with the server's authorization server per the
//     MCP authorization spec. This package stores no credentials; it asks
//     Nango whether a given user is connected.
//   - AuthTypeManual: the server needs no auth, and every member is
//     connected automatically.
//
// A server's name doubles as its ID and, in internal/mcpserver, the
// namespace its tools are exposed under, so it's restricted to a limited
// character set (see validateServerName) and unique within the org across
// both auth types. For OAuth servers, Nango's own integration key (needed to
// actually talk to Nango, and globally unique across the whole Nango
// environment, unlike name) is derived deterministically from the org and
// name and stored on the record.
package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/opensocial"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ErrManualServer is returned by the per-member authorization calls on a
// manually configured server, which has no per-member connection.
var ErrManualServer = errors.New("mcp server is configured manually and needs no sign-in")

// ErrNotConnected is returned when disconnecting from a server the caller has no
// connection to.
var ErrNotConnected = errors.New("not connected to mcp server")

// serverNamePattern restricts MCP server names to a limited, predictable
// character set: they double as this server's record key and, in
// internal/mcpserver, the namespace its tools are exposed under.
var serverNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ErrInvalidServerName is returned when a server name doesn't match
// serverNamePattern.
var ErrInvalidServerName = errors.New(
	"name must be 1-64 characters, using only letters, digits, hyphens, and underscores",
)

// ErrServerNameTaken is returned when adding a server whose name is already
// used by another server in the same org.
var ErrServerNameTaken = errors.New("a server with this name already exists for this org")

func validateServerName(name string) error {
	if !serverNamePattern.MatchString(name) {
		return ErrInvalidServerName
	}
	return nil
}

// NangoKeyFor deterministically derives a Nango integration unique_key for
// orgID's server named id. Combining the two guarantees global uniqueness
// (org DIDs are globally unique, and id is unique within an org) without
// needing to invent and thread through a separate identifier. Exported so
// tests outside this package can compute the same value.
func NangoKeyFor(orgID syntax.DID, id syntax.RecordKey) string {
	return orgID.String() + ":" + string(id)
}

// OrgMcpServerStore is the subset of opensocial.Store the gateway needs to
// read and write org MCP server configuration.
type OrgMcpServerStore interface {
	PutMcpServer(
		ctx context.Context,
		orgDID syntax.DID,
		server *opensocial.McpServer,
	) (*opensocial.McpServer, error)
	GetMcpServer(
		ctx context.Context,
		orgDID syntax.DID,
		id syntax.RecordKey,
	) (*opensocial.McpServer, error)
	ListMcpServers(ctx context.Context, orgDID syntax.DID) ([]*opensocial.McpServer, error)
	UpdateMcpServer(
		ctx context.Context,
		orgDID syntax.DID,
		id syntax.RecordKey,
		description, serverURL *string,
	) (*opensocial.McpServer, error)
	RemoveMcpServer(ctx context.Context, orgDID syntax.DID, id syntax.RecordKey) error
	// ListMemberSpaces lists the members spaces of every org user belongs
	// to.
	ListMemberSpaces(ctx context.Context, user syntax.DID) ([]habitat_syntax.SpaceURI, error)
}

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
	// A non-empty serverURL pre-fills the session's MCP server URL.
	CreateConnectSession(
		ctx context.Context,
		uniqueKey string,
		endUserID, orgID, serverURL string,
	) (string, error)
	// GetConnection fetches a Connection's live details, including the MCP
	// server URL it was made with.
	GetConnection(
		ctx context.Context,
		connectionID, providerConfigKey string,
	) (*nango.ConnectionDetails, error)
	// ListConnections lists the MCP connections tagged with endUserID.
	ListConnections(ctx context.Context, endUserID string) ([]nango.Connection, error)
	// DeleteConnection deletes a Nango Connection.
	DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error
}

// Server is an org's MCP server of either AuthType, as exposed through the
// API.
type Server struct {
	ID          syntax.RecordKey
	Name        string
	Description string
	AuthType    AuthType
}

func serverFromRecord(s *opensocial.McpServer) *Server {
	return &Server{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		AuthType:    authTypeOf(s.AuthType),
	}
}

// ServerWithStatus is an org's MCP server, along with whether a particular
// caller has connected to it. Manual servers are always connected.
type ServerWithStatus struct {
	Server    *Server
	Connected bool
}

// ServerUpdate is a partial update to an org's MCP server. Nil fields are
// left unchanged. URL applies only to manual servers.
type ServerUpdate struct {
	Description *string
	URL         *string
}

// Store manages MCP server configuration and per-user Nango connections for orgs.
type Store interface {
	// AddServer adds an MCP server to orgID, reached at serverURL, by
	// writing its record. For an OAuth server it also registers a Nango
	// Integration, which members then connect to with StartAuthorization.
	// name must match validateServerName and be unused within orgID.
	AddServer(
		ctx context.Context,
		orgID syntax.DID,
		name, description, serverURL string,
		authType AuthType,
	) (*Server, error)
	// UpdateServer updates an existing org MCP server. It returns
	// opensocial.ErrMcpServerNotFound if there's no such server.
	UpdateServer(
		ctx context.Context,
		orgID syntax.DID,
		id syntax.RecordKey,
		update ServerUpdate,
	) (*Server, error)
	// RemoveServer deletes an org's MCP server, and for an OAuth server its
	// Nango Integration.
	RemoveServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error
	// ListServers lists the MCP servers configured for an org, along with
	// whether did has connected to each one.
	ListServers(ctx context.Context, orgID syntax.DID, did syntax.DID) ([]*ServerWithStatus, error)

	// StartAuthorization begins a Nango Connect session for did to connect to
	// an existing org MCP server, returning a session token for the
	// frontend's Nango Connect UI.
	StartAuthorization(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		id syntax.RecordKey,
	) (sessionToken string, err error)
	// DisconnectServer deletes did's Nango connection to orgID's MCP server id.
	DisconnectServer(
		ctx context.Context,
		did syntax.DID,
		orgID syntax.DID,
		id syntax.RecordKey,
	) error
	// ListManualServersForMember lists the manual servers of every org did
	// belongs to, including their URLs, for pear's own MCP
	// client to connect with.
	ListManualServersForMember(ctx context.Context, did syntax.DID) ([]*ManualServer, error)
}

type store struct {
	nango   NangoClient
	records OrgMcpServerStore
}

// NewStore constructs a Store. nangoClient brokers the OAuth flow and
// connection state; records reads/writes server configuration.
func NewStore(nangoClient NangoClient, records OrgMcpServerStore) (Store, error) {
	if nangoClient == nil {
		return nil, fmt.Errorf("nango client is required")
	}
	if records == nil {
		return nil, fmt.Errorf("org mcp server store is required")
	}
	return &store{nango: nangoClient, records: records}, nil
}

// checkNameFree returns ErrServerNameTaken if orgID already has a server of
// either AuthType with the given id.
func (s *store) checkNameFree(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
	if _, err := s.records.GetMcpServer(ctx, orgID, id); err == nil {
		return fmt.Errorf("%w: %q", ErrServerNameTaken, id)
	} else if !errors.Is(err, opensocial.ErrMcpServerNotFound) {
		return fmt.Errorf("checking existing server: %w", err)
	}
	return nil
}

// getOAuth returns orgID's OAuth server id, or ErrManualServer if it's a
// manual server.
func (s *store) getOAuth(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
) (*opensocial.McpServer, error) {
	server, err := s.records.GetMcpServer(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if authTypeOf(server.AuthType) == AuthTypeManual {
		return nil, ErrManualServer
	}
	return server, nil
}

func (s *store) AddServer(
	ctx context.Context,
	orgID syntax.DID,
	name, description, serverURL string,
	authType AuthType,
) (*Server, error) {
	if err := validateServerName(name); err != nil {
		return nil, err
	}
	if err := validateServerURL(serverURL); err != nil {
		return nil, err
	}
	if authType != AuthTypeOAuth && authType != AuthTypeManual {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAuthType, authType)
	}
	id := syntax.RecordKey(name)
	if err := s.checkNameFree(ctx, orgID, id); err != nil {
		return nil, err
	}

	record := &opensocial.McpServer{
		ID:          id,
		Name:        name,
		Description: description,
		AuthType:    string(authType),
		ServerURL:   serverURL,
	}
	if authType == AuthTypeOAuth {
		record.NangoKey = NangoKeyFor(orgID, id)
		if err := s.nango.CreateIntegration(ctx, record.NangoKey); err != nil {
			return nil, fmt.Errorf("create nango integration: %w", err)
		}
	}
	server, err := s.records.PutMcpServer(ctx, orgID, record)
	if err != nil {
		if record.NangoKey != "" {
			_ = s.nango.DeleteIntegration(ctx, record.NangoKey)
		}
		return nil, err
	}
	return serverFromRecord(server), nil
}

func (s *store) UpdateServer(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
	update ServerUpdate,
) (*Server, error) {
	if update.URL != nil {
		// An OAuth server's URL is whatever its Nango connections were made
		// with, so it can't be changed here.
		existing, err := s.records.GetMcpServer(ctx, orgID, id)
		if err != nil {
			return nil, err
		}
		if authTypeOf(existing.AuthType) != AuthTypeManual {
			return nil, fmt.Errorf("url applies only to manual servers")
		}
		if err := validateServerURL(*update.URL); err != nil {
			return nil, err
		}
	}
	server, err := s.records.UpdateMcpServer(ctx, orgID, id, update.Description, update.URL)
	if err != nil {
		return nil, err
	}
	return serverFromRecord(server), nil
}

func (s *store) RemoveServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
	server, err := s.records.GetMcpServer(ctx, orgID, id)
	if err != nil {
		return err
	}
	if err := s.records.RemoveMcpServer(ctx, orgID, id); err != nil {
		return err
	}
	if authTypeOf(server.AuthType) == AuthTypeManual {
		return nil
	}
	if err := s.nango.DeleteIntegration(ctx, server.NangoKey); err != nil {
		return fmt.Errorf("delete nango integration: %w", err)
	}
	return nil
}

func (s *store) ListServers(
	ctx context.Context,
	orgID syntax.DID,
	did syntax.DID,
) ([]*ServerWithStatus, error) {
	servers, err := s.records.ListMcpServers(ctx, orgID)
	if err != nil {
		return nil, err
	}

	var connected map[string]bool
	if slices.ContainsFunc(servers, func(server *opensocial.McpServer) bool {
		return authTypeOf(server.AuthType) == AuthTypeOAuth
	}) {
		connections, err := s.nango.ListConnections(ctx, did.String())
		if err != nil {
			return nil, fmt.Errorf("list nango connections: %w", err)
		}
		connected = make(map[string]bool, len(connections))
		for _, conn := range connections {
			connected[conn.ProviderConfigKey] = true
		}
	}

	out := make([]*ServerWithStatus, len(servers))
	for i, server := range servers {
		out[i] = &ServerWithStatus{
			Server: serverFromRecord(server),
			Connected: authTypeOf(server.AuthType) == AuthTypeManual ||
				connected[server.NangoKey],
		}
	}
	slices.SortFunc(out, func(a, b *ServerWithStatus) int {
		return strings.Compare(string(a.Server.ID), string(b.Server.ID))
	})
	return out, nil
}

func (s *store) StartAuthorization(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	id syntax.RecordKey,
) (string, error) {
	server, err := s.getOAuth(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	token, err := s.nango.CreateConnectSession(
		ctx, server.NangoKey, did.String(), orgID.String(), server.ServerURL,
	)
	if err != nil {
		return "", fmt.Errorf("create nango connect session: %w", err)
	}
	return token, nil
}

func (s *store) DisconnectServer(
	ctx context.Context, did syntax.DID, orgID syntax.DID, id syntax.RecordKey,
) error {
	server, err := s.getOAuth(ctx, orgID, id)
	if err != nil {
		return err
	}
	connections, err := s.nango.ListConnections(ctx, did.String())
	if err != nil {
		return fmt.Errorf("list nango connections: %w", err)
	}
	for _, conn := range connections {
		if conn.ProviderConfigKey == server.NangoKey {
			if err := s.nango.DeleteConnection(
				ctx,
				conn.ConnectionID,
				server.NangoKey,
			); err != nil {
				return fmt.Errorf("delete nango connection: %w", err)
			}
			return nil
		}
	}
	return ErrNotConnected
}

func (s *store) ListManualServersForMember(
	ctx context.Context,
	did syntax.DID,
) ([]*ManualServer, error) {
	memberSpaces, err := s.records.ListMemberSpaces(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("list member spaces: %w", err)
	}
	var out []*ManualServer
	for _, space := range memberSpaces {
		orgID := space.SpaceOwner()
		servers, err := s.records.ListMcpServers(ctx, orgID)
		if err != nil {
			return nil, err
		}
		for _, server := range servers {
			if authTypeOf(server.AuthType) != AuthTypeManual {
				continue
			}
			out = append(out, &ManualServer{
				OrgID:       orgID,
				ID:          server.ID,
				Description: server.Description,
				URL:         server.ServerURL,
			})
		}
	}
	return out, nil
}
