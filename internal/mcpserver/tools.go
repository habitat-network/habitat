package mcpserver

import (
	"context"
	"fmt"

	habitat_err "github.com/habitat-network/habitat/internal/error"
	"github.com/habitat-network/habitat/internal/permissions"
	"github.com/habitat-network/habitat/internal/repo"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type getRecordInput struct {
	URI string `json:"uri" jsonschema:"the habitat:// URI of the record to fetch, e.g. habitat://did:plc:abc/network.habitat.example/3jz"`
}

type getRecordOutput struct {
	URI   string `json:"uri"`
	Value any    `json:"value"`
}

// getRecordHandler implements the "get_record" MCP tool. It mirrors
// internal/pear's getRecordLocal: check the caller's permission on the
// target record, then read it straight from the repo store.
func getRecordHandler(
	store repo.Repo,
	perms permissions.Store,
) mcp.ToolHandlerFor[getRecordInput, getRecordOutput] {
	return func(
		ctx context.Context,
		_ *mcp.CallToolRequest,
		input getRecordInput,
	) (*mcp.CallToolResult, getRecordOutput, error) {
		tokenInfo := auth.TokenInfoFromContext(ctx)
		if tokenInfo == nil || tokenInfo.UserID == "" {
			return nil, getRecordOutput{}, fmt.Errorf("missing authenticated caller")
		}
		caller := syntax.DID(tokenInfo.UserID)

		uri, err := habitat_syntax.ParseHabitatURI(input.URI)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("invalid uri: %w", err)
		}
		owner, collection, rkey, err := uri.ExtractParts()
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("invalid uri: %w", err)
		}

		ok, err := perms.HasPermission(ctx, caller, owner, collection, rkey)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("checking permission: %w", err)
		}
		if !ok {
			return nil, getRecordOutput{}, habitat_err.ErrUnauthorized
		}

		record, err := store.GetRecord(ctx, owner.String(), collection.String(), rkey.String())
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("getting record: %w", err)
		}

		return nil, getRecordOutput{URI: input.URI, Value: record.Value}, nil
	}
}
