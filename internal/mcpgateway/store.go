// Package mcpgateway lets org admins configure MCP (Model Context Protocol)
// servers for their org, and lets org members opt in to authorizing with
// those servers. MCP server configuration lives as network.habitat.mcp.server
// records in the org's opensocial members space (see internal/opensocial);
// authorization is brokered entirely through Nango
// (https://nango.dev/docs/guides/auth/mcp-auth) via its mcp-generic
// connector, which asks the connecting user for the server's URL and
// discovers/registers with its authorization server per the MCP
// authorization spec. This package stores nothing of the server's own URL or
// credentials; it only records the org's chosen name/description for it and
// asks Nango whether a given user is connected.
//
// A server's name doubles as its record key and, in internal/mcpserver, the
// namespace its tools are exposed under, so it's restricted to a limited
// character set (see validateServerName) and unique within the org. Nango's
// own integration key (needed to actually talk to Nango, and globally
// unique across the whole Nango environment, unlike name) is derived
// deterministically from the org and name and stored on the record.
package mcpgateway

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/nango"
	"github.com/habitat-network/habitat/internal/opensocial"
)

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
		name, description, nangoKey string,
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
	CreateConnectSession(ctx context.Context, uniqueKey string, endUserID, orgID string) (string, error)
	// ListConnections lists the MCP connections tagged with endUserID.
	ListConnections(ctx context.Context, endUserID string) ([]nango.Connection, error)
	// DeleteConnection deletes a Nango Connection.
	DeleteConnection(ctx context.Context, connectionID, providerConfigKey string) error
}

// ServerWithStatus is an org's MCP server, along with whether a particular
// caller has connected to it.
type ServerWithStatus struct {
	Server    *opensocial.McpServer
	Connected bool
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
	) (*opensocial.McpServer, error)
	// CancelAddServer abandons an in-progress BeginAddServer flow, deleting
	// its Nango Integration.
	CancelAddServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error
	// UpdateServer updates an existing org MCP server's description. A nil
	// description is left unchanged.
	UpdateServer(
		ctx context.Context,
		orgID syntax.DID,
		id syntax.RecordKey,
		description *string,
	) (*opensocial.McpServer, error)
	// RemoveServer deletes an org's MCP server and its Nango Integration.
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
	DisconnectServer(ctx context.Context, did syntax.DID, orgID syntax.DID, id syntax.RecordKey) error
}

type store struct {
	nango   NangoClient
	records OrgMcpServerStore
}

// NewStore constructs a Store. nangoClient brokers the OAuth flow and
// connection state; records reads/writes org MCP server configuration.
func NewStore(nangoClient NangoClient, records OrgMcpServerStore) (Store, error) {
	if nangoClient == nil {
		return nil, fmt.Errorf("nango client is required")
	}
	if records == nil {
		return nil, fmt.Errorf("org mcp server store is required")
	}
	return &store{nango: nangoClient, records: records}, nil
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
	if _, err := s.records.GetMcpServer(ctx, orgID, id); err == nil {
		return "", "", fmt.Errorf("%w: %q", ErrServerNameTaken, name)
	} else if !errors.Is(err, opensocial.ErrMcpServerNotFound) {
		return "", "", fmt.Errorf("checking existing server: %w", err)
	}

	nangoKey := NangoKeyFor(orgID, id)
	if err := s.nango.CreateIntegration(ctx, nangoKey); err != nil {
		return "", "", fmt.Errorf("create nango integration: %w", err)
	}
	token, err := s.nango.CreateConnectSession(ctx, nangoKey, did.String(), orgID.String())
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
) (*opensocial.McpServer, error) {
	nangoKey := NangoKeyFor(orgID, id)
	connections, err := s.nango.ListConnections(ctx, did.String())
	if err != nil {
		return nil, fmt.Errorf("list nango connections: %w", err)
	}
	connected := false
	for _, conn := range connections {
		if conn.ProviderConfigKey == nangoKey {
			connected = true
			break
		}
	}
	if !connected {
		return nil, fmt.Errorf("no nango connection found for server %q", id)
	}
	return s.records.PutMcpServer(ctx, orgID, id, name, description, nangoKey)
}

func (s *store) CancelAddServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
	if err := s.nango.DeleteIntegration(ctx, NangoKeyFor(orgID, id)); err != nil {
		return fmt.Errorf("delete nango integration: %w", err)
	}
	return nil
}

func (s *store) UpdateServer(
	ctx context.Context,
	orgID syntax.DID,
	id syntax.RecordKey,
	description *string,
) (*opensocial.McpServer, error) {
	return s.records.UpdateMcpServer(ctx, orgID, id, description)
}

func (s *store) RemoveServer(ctx context.Context, orgID syntax.DID, id syntax.RecordKey) error {
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
	connections, err := s.nango.ListConnections(ctx, did.String())
	if err != nil {
		return nil, fmt.Errorf("list nango connections: %w", err)
	}
	connected := make(map[string]bool, len(connections))
	for _, conn := range connections {
		connected[conn.ProviderConfigKey] = true
	}

	out := make([]*ServerWithStatus, len(servers))
	for i, server := range servers {
		out[i] = &ServerWithStatus{Server: server, Connected: connected[server.NangoKey]}
	}
	return out, nil
}

func (s *store) StartAuthorization(
	ctx context.Context,
	did syntax.DID,
	orgID syntax.DID,
	id syntax.RecordKey,
) (string, error) {
	server, err := s.records.GetMcpServer(ctx, orgID, id)
	if err != nil {
		return "", err
	}
	token, err := s.nango.CreateConnectSession(ctx, server.NangoKey, did.String(), orgID.String())
	if err != nil {
		return "", fmt.Errorf("create nango connect session: %w", err)
	}
	return token, nil
}

func (s *store) DisconnectServer(
	ctx context.Context, did syntax.DID, orgID syntax.DID, id syntax.RecordKey,
) error {
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
			if err := s.nango.DeleteConnection(ctx, conn.ConnectionID, server.NangoKey); err != nil {
				return fmt.Errorf("delete nango connection: %w", err)
			}
			return nil
		}
	}
	return ErrNotConnected
}
