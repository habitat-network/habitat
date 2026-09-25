// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members opt in to authorizing with
// those servers. A server uses one of two AuthTypes:
//
//   - AuthTypeOAuth: configuration lives as network.habitat.mcp.server
//     records in the org's opensocial members space (see
//     internal/opensocial); authorization is brokered entirely through
//     Nango (https://nango.dev/docs/guides/auth/mcp-auth) via its
//     mcp-generic connector, which asks the connecting user for the
//     server's URL and discovers/registers with its authorization server
//     per the MCP authorization spec. This package stores nothing of the
//     server's own URL or credentials; it only records the org's chosen
//     name/description for it and asks Nango whether a given user is
//     connected.
//   - AuthTypeManual: an admin enters the server's URL and any static
//     headers (or none, for servers without auth) once for the whole org.
//     These live, encrypted, in pear's own database (see
//     ManualServerStore), and every member is connected automatically.
//
// A server's name doubles as its ID and, in internal/mcpserver, the
// namespace its tools are exposed under, so it's restricted to a limited
// character set (see validateServerName) and unique within the org across
// both auth types. Nango's own integration key (needed to actually talk to
// Nango, and globally unique across the whole Nango environment, unlike
// name) is derived deterministically from the org and name and stored on
// the record.
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
		id syntax.RecordKey,
		name, description, nangoKey, serverURL string,
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
		description *string,
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
// API. It never carries a manual server's URL or headers.
type Server struct {
	ID          syntax.RecordKey
	Name        string
	Description string
	AuthType    AuthType
}

func serverFromOAuth(s *opensocial.McpServer) *Server {
	return &Server{ID: s.ID, Name: s.Name, Description: s.Description, AuthType: AuthTypeOAuth}
}

func serverFromManual(s *ManualServer) *Server {
	return &Server{
		ID:          s.ID,
		Name:        string(s.ID),
		Description: s.Description,
		AuthType:    AuthTypeManual,
	}
}

// ServerWithStatus is an org's MCP server, along with whether a particular
// caller has connected to it. Manual servers are always connected.
type ServerWithStatus struct {
	Server    *Server
	Connected bool
}

// ServerUpdate is a partial update to an org's MCP server. Nil fields are
// left unchanged. URL and Headers apply only to manual servers; a non-nil
// Headers replaces all of the server's headers.
type ServerUpdate struct {
	Description *string
	URL         *string
	Headers     map[string]string
}

// Store manages MCP server configuration and per-user Nango connections for orgs.
type Store interface {
	// BeginAddServer starts configuring a new MCP server: it registers a
	// Nango Integration for it and opens a Connect session for did (the
	// admin doing the adding), returning the new server's ID (equal to
	// name) and a session token for the frontend's Nango Connect UI. No org
	// record is written yet; call CompleteAddServer once Nango reports
	// success, or CancelAddServer if the admin abandons it. name must match
	// validateServerName and be unused within orgID.
	BeginAddServer(
		ctx context.Context,
		orgID syntax.DID,
		did syntax.DID,
		name, description string,
	) (id syntax.RecordKey, sessionToken string, err error)
	// CompleteAddServer writes the org record for a server previously
	// started with BeginAddServer, once the admin has completed
	// authorization in Nango's Connect UI.
	CompleteAddServer(
		ctx context.Context,
		orgID syntax.DID,
		did syntax.DID,
		id syntax.RecordKey,
		name, description string,
	) (*Server, error)
	// CancelAddServer abandons an in-progress BeginAddServer flow, deleting
	// its Nango Integration.
	CancelAddServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error
	// AddManualServer configures a new manual MCP server for orgID, reached
	// at serverURL with headers sent on every request. name must match
	// validateServerName and be unused within orgID.
	AddManualServer(
		ctx context.Context,
		orgID syntax.DID,
		name, description, serverURL string,
		headers map[string]string,
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
	// belongs to, including their URLs and headers, for pear's own MCP
	// client to connect with.
	ListManualServersForMember(ctx context.Context, did syntax.DID) ([]*ManualServer, error)
}

type store struct {
	nango   NangoClient
	records OrgMcpServerStore
	manual  ManualServerStore
}

// NewStore constructs a Store. nangoClient brokers the OAuth flow and
// connection state; records reads/writes OAuth server configuration; manual
// reads/writes manual server configuration.
func NewStore(
	nangoClient NangoClient,
	records OrgMcpServerStore,
	manual ManualServerStore,
) (Store, error) {
	if nangoClient == nil {
		return nil, fmt.Errorf("nango client is required")
	}
	if records == nil {
		return nil, fmt.Errorf("org mcp server store is required")
	}
	if manual == nil {
		return nil, fmt.Errorf("manual mcp server store is required")
	}
	return &store{nango: nangoClient, records: records, manual: manual}, nil
}

// checkNameFree returns ErrServerNameTaken if orgID already has a server of
// either AuthType with the given id.
func (s *store) checkNameFree(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
	if _, err := s.records.GetMcpServer(ctx, orgID, id); err == nil {
		return fmt.Errorf("%w: %q", ErrServerNameTaken, id)
	} else if !errors.Is(err, opensocial.ErrMcpServerNotFound) {
		return fmt.Errorf("checking existing server: %w", err)
	}
	if _, err := s.manual.Get(ctx, orgID, id); err == nil {
		return fmt.Errorf("%w: %q", ErrServerNameTaken, id)
	} else if !errors.Is(err, ErrManualServerNotFound) {
		return fmt.Errorf("checking existing manual server: %w", err)
	}
	return nil
}

// getManual returns orgID's manual server id, or nil if there's none.
func (s *store) getManual(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
) (*ManualServer, error) {
	server, err := s.manual.Get(ctx, orgID, id)
	if errors.Is(err, ErrManualServerNotFound) {
		return nil, nil
	}
	return server, err
}

func (s *store) BeginAddServer(
	ctx context.Context,
	orgID syntax.DID,
	did syntax.DID,
	name, description string,
) (syntax.RecordKey, string, error) {
	if err := validateServerName(name); err != nil {
		return "", "", err
	}
	id := syntax.RecordKey(name)
	if err := s.checkNameFree(ctx, orgID, id); err != nil {
		return "", "", err
	}

	nangoKey := NangoKeyFor(orgID, id)
	if err := s.nango.CreateIntegration(ctx, nangoKey); err != nil {
		return "", "", fmt.Errorf("create nango integration: %w", err)
	}
	token, err := s.nango.CreateConnectSession(ctx, nangoKey, did.String(), orgID.String(), "")
	if err != nil {
		_ = s.nango.DeleteIntegration(ctx, nangoKey)
		return "", "", fmt.Errorf("create nango connect session: %w", err)
	}
	return id, token, nil
}

func (s *store) CompleteAddServer(
	ctx context.Context,
	orgID syntax.DID,
	did syntax.DID,
	id syntax.RecordKey,
	name, description string,
) (*Server, error) {
	nangoKey := NangoKeyFor(orgID, id)
	connections, err := s.nango.ListConnections(ctx, did.String())
	if err != nil {
		return nil, fmt.Errorf("list nango connections: %w", err)
	}
	var adminConn *nango.Connection
	for _, conn := range connections {
		if conn.ProviderConfigKey == nangoKey {
			adminConn = &conn
			break
		}
	}
	if adminConn == nil {
		return nil, fmt.Errorf("no nango connection found for server %q", id)
	}
	// Record the URL the admin entered, so members connecting later don't
	// have to enter it again (see StartAuthorization).
	details, err := s.nango.GetConnection(ctx, adminConn.ConnectionID, nangoKey)
	if err != nil {
		return nil, fmt.Errorf("get nango connection: %w", err)
	}
	server, err := s.records.PutMcpServer(
		ctx, orgID, id, name, description, nangoKey, details.MCPServerURL,
	)
	if err != nil {
		return nil, err
	}
	return serverFromOAuth(server), nil
}

func (s *store) CancelAddServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
	if err := s.nango.DeleteIntegration(ctx, NangoKeyFor(orgID, id)); err != nil {
		return fmt.Errorf("delete nango integration: %w", err)
	}
	return nil
}

func (s *store) AddManualServer(
	ctx context.Context,
	orgID syntax.DID,
	name, description, serverURL string,
	headers map[string]string,
) (*Server, error) {
	if err := validateServerName(name); err != nil {
		return nil, err
	}
	if err := validateManualConfig(serverURL, headers); err != nil {
		return nil, err
	}
	id := syntax.RecordKey(name)
	if err := s.checkNameFree(ctx, orgID, id); err != nil {
		return nil, err
	}
	server := &ManualServer{
		OrgID:       orgID,
		ID:          id,
		Description: description,
		URL:         serverURL,
		Headers:     headers,
	}
	if err := s.manual.Put(ctx, server); err != nil {
		return nil, err
	}
	return serverFromManual(server), nil
}

func (s *store) UpdateServer(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
	update ServerUpdate,
) (*Server, error) {
	manual, err := s.getManual(ctx, orgID, id)
	if err != nil {
		return nil, err
	}
	if manual == nil {
		if update.URL != nil || update.Headers != nil {
			return nil, fmt.Errorf("url and headers apply only to manual servers")
		}
		server, err := s.records.UpdateMcpServer(ctx, orgID, id, update.Description)
		if err != nil {
			return nil, err
		}
		return serverFromOAuth(server), nil
	}

	if update.Description != nil {
		manual.Description = *update.Description
	}
	if update.URL != nil {
		manual.URL = *update.URL
	}
	if update.Headers != nil {
		manual.Headers = update.Headers
	}
	if err := validateManualConfig(manual.URL, manual.Headers); err != nil {
		return nil, err
	}
	if err := s.manual.Put(ctx, manual); err != nil {
		return nil, err
	}
	return serverFromManual(manual), nil
}

func (s *store) RemoveServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
	if err := s.manual.Delete(ctx, orgID, id); err == nil {
		return nil
	} else if !errors.Is(err, ErrManualServerNotFound) {
		return err
	}
	server, err := s.records.GetMcpServer(ctx, orgID, id)
	if err != nil {
		return err
	}
	if err := s.records.RemoveMcpServer(ctx, orgID, id); err != nil {
		return err
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
	manual, err := s.manual.List(ctx, orgID)
	if err != nil {
		return nil, err
	}

	out := make([]*ServerWithStatus, 0, len(servers)+len(manual))
	if len(servers) > 0 {
		connections, err := s.nango.ListConnections(ctx, did.String())
		if err != nil {
			return nil, fmt.Errorf("list nango connections: %w", err)
		}
		connected := make(map[string]bool, len(connections))
		for _, conn := range connections {
			connected[conn.ProviderConfigKey] = true
		}
		for _, server := range servers {
			out = append(out, &ServerWithStatus{
				Server:    serverFromOAuth(server),
				Connected: connected[server.NangoKey],
			})
		}
	}
	for _, server := range manual {
		out = append(out, &ServerWithStatus{Server: serverFromManual(server), Connected: true})
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
	if manual, err := s.getManual(ctx, orgID, id); err != nil {
		return "", err
	} else if manual != nil {
		return "", ErrManualServer
	}
	server, err := s.records.GetMcpServer(ctx, orgID, id)
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
	if manual, err := s.getManual(ctx, orgID, id); err != nil {
		return err
	} else if manual != nil {
		return ErrManualServer
	}
	server, err := s.records.GetMcpServer(ctx, orgID, id)
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
		servers, err := s.manual.List(ctx, space.SpaceOwner())
		if err != nil {
			return nil, err
		}
		out = append(out, servers...)
	}
	return out, nil
}
