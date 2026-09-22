package mcpgateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/oauthex"
)

// detectAuth probes mcpServerURL per the MCP authorization spec
// (https://modelcontextprotocol.io/docs/tutorials/security/authorization):
// a server that requires authorization must reject an unauthenticated
// request with 401 and a "WWW-Authenticate: Bearer ..." challenge. Any other
// response means the server doesn't require per-user authorization.
//
// Discovering and registering with the server's own authorization server is
// delegated to Nango's mcp-generic provider rather than done here; this
// probe only decides whether that's necessary at all.
func (s *store) detectAuth(ctx context.Context, mcpServerURL string) (AuthType, error) {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, mcpServerURL, strings.NewReader("{}"),
	)
	if err != nil {
		return "", fmt.Errorf("build probe request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("probing mcp server: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		return AuthTypeNone, nil
	}

	challenges, err := oauthex.ParseWWWAuthenticate(resp.Header.Values("WWW-Authenticate"))
	if err != nil || !hasBearerChallenge(challenges) {
		return AuthTypeNone, nil
	}
	return AuthTypeOAuth, nil
}

func hasBearerChallenge(challenges []oauthex.Challenge) bool {
	for _, c := range challenges {
		if c.Scheme == "bearer" {
			return true
		}
	}
	return false
}
