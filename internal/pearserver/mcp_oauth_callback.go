package pearserver

import (
	"errors"
	"html/template"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/mcpgateway"
)

// MCPOAuthCallbackPath is where MCP servers' authorization servers redirect a
// member's browser once they've authorized pear (see
// network.habitat.mcp.startAuthorization). pear registers its absolute URL as
// its OAuth redirect URI.
const MCPOAuthCallbackPath = "/mcp-oauth/callback"

// mcpOAuthCallbackMessageType is the type of the message the callback page
// posts to the window that opened it, so the management frontend can refresh
// the server's connected status.
const mcpOAuthCallbackMessageType = "habitat-mcp-oauth"

var mcpOAuthCallbackPage = template.Must(template.New("callback").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{if .OK}}Connected{{else}}Couldn't connect{{end}}</title>
<style>
  body { font-family: system-ui, sans-serif; margin: 0; min-height: 100vh;
    display: flex; align-items: center; justify-content: center;
    background: #fafafa; color: #18181b; }
  @media (prefers-color-scheme: dark) { body { background: #18181b; color: #fafafa; } }
  main { max-width: 28rem; padding: 1.5rem; text-align: center; }
</style>
</head>
<body>
<main>
  <h1>{{if .OK}}Connected{{else}}Couldn't connect{{end}}</h1>
  <p>{{.Message}}</p>
  <p>You can close this window.</p>
</main>
<script>
  if (window.opener) {
    window.opener.postMessage({ type: {{.MessageType}}, ok: {{.OK}} }, "*");
    {{if .OK}}window.close();{{end}}
  }
</script>
</body>
</html>
`))

// McpOAuthCallback finishes a member's authorization with an MCP server: the
// authorization server redirects their browser here with the authorization
// response, and pear exchanges it for the member's tokens. The member is
// identified by the state they started with, so no session is needed.
func (p *PearServer) McpOAuthCallback(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()
	status, message := http.StatusOK, "Habitat can now use this server on your behalf."
	if oauthErr := q.Get("error"); oauthErr != "" {
		// The member declined, or the authorization server refused. The state
		// is left to expire.
		status, message = http.StatusBadRequest, "The server didn't grant access ("+oauthErr+")."
	} else if err := p.mcpGatewayStore.CompleteAuthorization(
		ctx, q.Get("state"), q.Get("code"), q.Get("iss"),
	); errors.Is(err, mcpgateway.ErrAuthorizationNotFound) {
		status, message = http.StatusBadRequest,
			"This sign-in link has expired or was already used. Try connecting again."
	} else if errors.Is(err, mcpgateway.ErrServerNotFound) {
		status, message = http.StatusNotFound, "This server was removed from your organization."
	} else if errors.Is(err, mcpgateway.ErrAuthorizationFailed) {
		slog.WarnContext(ctx, "mcp oauth callback", "err", err)
		status, message = http.StatusBadRequest, "The server's response couldn't be verified."
	} else if err != nil {
		slog.ErrorContext(ctx, "mcp oauth callback", "err", err)
		status, message = http.StatusInternalServerError, "Something went wrong. Try again."
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.WriteHeader(status)
	if err := mcpOAuthCallbackPage.Execute(w, struct {
		OK          bool
		Message     string
		MessageType string
	}{status == http.StatusOK, message, mcpOAuthCallbackMessageType}); err != nil {
		slog.ErrorContext(ctx, "render mcp oauth callback", "err", err)
	}
}
