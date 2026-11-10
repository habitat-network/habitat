package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// mcpDebugTimeout bounds one debug request, which may call a slow tool.
const mcpDebugTimeout = 60 * time.Second

// McpDebug implements network.habitat.mcp.debug.
func (p *PearServer) McpDebug(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return
	}

	var input habitat.NetworkHabitatMcpDebugInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "reading request body", err)
		return
	}
	org, ok := httpx.ParseDIDInput(ctx, w, input.Org, "org")
	if !ok {
		return
	}
	if input.Id == "" || input.Method == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required fields", nil)
		return
	}
	if !p.requireMember(ctx, w, org, credInfo.Subject) {
		return
	}

	servers, err := p.mcpGatewayStore.ListServers(ctx, org, credInfo.Subject)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list mcp servers: %w", err))
		return
	}
	var server *mcpgateway.Server
	for _, s := range servers {
		if string(s.Server.Name) == input.Id {
			server = s.Server
		}
	}
	if server == nil {
		httpx.WriteError(ctx, w, "NotFound", "mcp server not found", http.StatusNotFound)
		return
	}

	var arguments any
	if input.Arguments != "" {
		if err := json.Unmarshal([]byte(input.Arguments), &arguments); err != nil {
			httpx.WriteInvalidRequest(ctx, w, "arguments must be valid JSON", err)
			return
		}
	}

	ctx, cancel := context.WithTimeout(ctx, mcpDebugTimeout)
	defer cancel()
	res, err := mcpgateway.Debug(ctx, p.mcpGatewayStore, credInfo.Subject, server,
		mcpgateway.DebugRequest{
			Method:    mcpgateway.DebugMethod(input.Method),
			Name:      input.Name,
			Arguments: arguments,
			URI:       input.Uri,
		})
	if errors.Is(err, mcpgateway.ErrInvalidDebugRequest) {
		httpx.WriteInvalidRequest(ctx, w, "debug mcp server", err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("debug mcp server: %w", err))
		return
	}

	out := habitat.NetworkHabitatMcpDebugOutput{
		Ok:         res.Err == "",
		DurationMs: res.DurationMs,
		Error:      res.Err,
		ErrorKind:  string(res.ErrKind),
	}
	if res.Err == "" {
		result, err := json.MarshalIndent(res.Result, "", "  ")
		if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("encode mcp result: %w", err))
			return
		}
		out.Result = string(result)
	}
	httpx.WriteJSON(ctx, w, out)
}
