package pearserver

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/internal/httpx"
)

// McpOAuthCallback is the OAuth redirect_uri registered with MCP servers'
// authorization servers. It completes the authorization-code flow begun by
// StartAuthorization and sends the caller's browser back to the returnURL
// it supplied there.
func (p *PearServer) McpOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	if authErr := q.Get("error"); authErr != "" {
		httpx.WriteInvalidRequest(
			ctx, w, "mcp authorization failed", fmt.Errorf("%s: %s", authErr, q.Get("error_description")),
		)
		return
	}

	code := q.Get("code")
	state := q.Get("state")
	if code == "" || state == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing code or state", nil)
		return
	}

	_, _, _, returnURL, err := p.mcpGatewayStore.CompleteAuthorization(ctx, state, code)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("complete mcp authorization: %w", err))
		return
	}

	http.Redirect(w, r, returnURL, http.StatusFound)
}
