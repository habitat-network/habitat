package mcpserver

import (
	"context"
	"fmt"

	habitat_err "github.com/habitat-network/habitat/internal/error"
	"github.com/habitat-network/habitat/internal/perms"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type getRecordInput struct {
	URI string `json:"uri" jsonschema:"the space record URI of the record to fetch, e.g. at://did:plc:abc/space/network.habitat.example/3jz/did:plc:abc/network.habitat.example/3jz"`
}

type getRecordOutput struct {
	URI   string `json:"uri"`
	Value any    `json:"value"`
}

// getRecordHandler implements the "get_record" MCP tool: check the caller
// holds at least a reader role on the record's space, then read the record
// straight from the space store.
func getRecordHandler(
	store spaces.Store,
	permStore perms.Store,
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

		recordURI, err := habitat_syntax.ParseSpaceRecordURI(input.URI)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("invalid uri: %w", err)
		}
		spaceURI := recordURI.SpaceURI()
		owner := recordURI.Repo()
		collection := recordURI.Collection()
		rkey := recordURI.Rkey()

		ok, err := permStore.CheckUserHasSpaceRole(
			ctx, caller, spaceURI, habitat_syntax.SpaceRoleReader,
		)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("checking permission: %w", err)
		}
		if !ok {
			return nil, getRecordOutput{}, habitat_err.ErrUnauthorized
		}

		record, err := store.GetRecord(ctx, spaceURI, owner, collection, rkey)
		if err != nil {
			return nil, getRecordOutput{}, fmt.Errorf("getting record: %w", err)
		}

		return nil, getRecordOutput{URI: input.URI, Value: record.Value}, nil
	}
}
