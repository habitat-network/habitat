package opensocial

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_api "github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// McpServerCollection is the collection MCP server records are written to,
// in the org's members space.
const McpServerCollection = "network.habitat.mcp.server"

// ErrMcpServerNotFound is returned when no MCP server matches the given ID/org.
var ErrMcpServerNotFound = errors.New("mcp server not found")

// McpServer is an MCP server configured for a community. Authorization
// against the server itself (including its URL) is handled by Nango.
type McpServer struct {
	// ID is this record's key. It's equal to Name, chosen once at creation
	// and immutable afterward, since it also namespaces the server's tools
	// as exposed by internal/mcpserver.
	ID          syntax.RecordKey
	Name        string
	Description string
	// NangoKey is the Nango integration's unique_key. Unlike Name, it's
	// globally unique across the whole Nango environment, not just this org.
	NangoKey string
}

func mcpServerFromRecord(rkey syntax.RecordKey, r habitat_api.NetworkHabitatMcpServer) *McpServer {
	return &McpServer{
		ID:          rkey,
		Name:        r.Name,
		Description: r.Description,
		NangoKey:    r.NangoKey,
	}
}

// PutMcpServer writes an MCP server record for orgDID at the given ID,
// creating or overwriting it. The ID is caller-chosen, not generated here.
func (s *Store) PutMcpServer(
	ctx context.Context,
	orgDID syntax.DID,
	id syntax.RecordKey,
	name, description, nangoKey string,
) (*McpServer, error) {
	record := habitat_api.NetworkHabitatMcpServer{
		Name:        name,
		Description: description,
		NangoKey:    nangoKey,
		UpdatedAt:   time.Now().Format(time.RFC3339),
	}
	recordBytes, err := spaces.MarshalRecord(record)
	if err != nil {
		return nil, fmt.Errorf("marshal mcp server record: %w", err)
	}
	if _, _, err := s.spacesStore.PutRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		McpServerCollection,
		id,
		recordBytes,
	); err != nil {
		return nil, fmt.Errorf("put mcp server record: %w", err)
	}
	return &McpServer{ID: id, Name: name, Description: description, NangoKey: nangoKey}, nil
}

// GetMcpServer fetches a single MCP server by ID, scoped to the org.
func (s *Store) GetMcpServer(
	ctx context.Context,
	orgDID syntax.DID,
	id syntax.RecordKey,
) (*McpServer, error) {
	record, err := s.spacesStore.GetRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		McpServerCollection,
		id,
	)
	if errors.Is(err, spaces.ErrRecordNotFound) {
		return nil, ErrMcpServerNotFound
	} else if err != nil {
		return nil, fmt.Errorf("get mcp server record: %w", err)
	}
	var r habitat_api.NetworkHabitatMcpServer
	if err := decodeRecordValue(record.Value, &r); err != nil {
		return nil, fmt.Errorf("decode mcp server record: %w", err)
	}
	return mcpServerFromRecord(record.Rkey, r), nil
}

// ListMcpServers lists the MCP servers configured for orgDID.
func (s *Store) ListMcpServers(ctx context.Context, orgDID syntax.DID) ([]*McpServer, error) {
	collection := syntax.NSID(McpServerCollection)
	records, err := s.spacesStore.ListRecords(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		&collection,
	)
	if err != nil {
		return nil, fmt.Errorf("list mcp server records: %w", err)
	}
	servers := make([]*McpServer, len(records))
	for i, record := range records {
		var r habitat_api.NetworkHabitatMcpServer
		if err := decodeRecordValue(record.Value, &r); err != nil {
			return nil, fmt.Errorf("decode mcp server record: %w", err)
		}
		servers[i] = mcpServerFromRecord(record.Rkey, r)
	}
	return servers, nil
}

// UpdateMcpServer updates an existing org MCP server's description. Name and
// NangoKey are immutable once set, since Name is this record's key.
func (s *Store) UpdateMcpServer(
	ctx context.Context,
	orgDID syntax.DID,
	id syntax.RecordKey,
	description *string,
) (*McpServer, error) {
	existing, err := s.GetMcpServer(ctx, orgDID, id)
	if err != nil {
		return nil, err
	}
	if description != nil {
		existing.Description = *description
	}
	return s.PutMcpServer(ctx, orgDID, id, existing.Name, existing.Description, existing.NangoKey)
}

// RemoveMcpServer deletes an org's MCP server record.
func (s *Store) RemoveMcpServer(ctx context.Context, orgDID syntax.DID, id syntax.RecordKey) error {
	if _, err := s.GetMcpServer(ctx, orgDID, id); err != nil {
		return err
	}
	if err := s.spacesStore.DeleteRecord(
		ctx,
		habitat_syntax.ConstructSpaceURI(orgDID, MembersSpaceType, "self"),
		orgDID,
		McpServerCollection,
		string(id),
	); err != nil {
		return fmt.Errorf("delete mcp server record: %w", err)
	}
	return nil
}
