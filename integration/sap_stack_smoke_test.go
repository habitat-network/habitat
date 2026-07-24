package integration

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSapStackBringsUp is a smoke test proving the full sap integration
// stack (postgres, Caddy, CoreDNS, toxiproxy, pear, sap) comes up healthy end
// to end: it verifies both pear's and sap's health endpoints respond 200 over
// their published host ports. This exercises start-ordering, CoreDNS
// resolution, Caddy TLS, and CA trust all at once, ahead of tests that drive
// an actual OAuth/notify flow across the stack.
func TestSapStackBringsUp(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	stack := startSapStack(ctx, t)

	checkHealth := func(name, url string) {
		t.Helper()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url+"/health", nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err, "%s health check request failed", name)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode, "%s health check returned non-200", name)
	}

	checkHealth("pear", stack.PearHTTP)
	checkHealth("sap", stack.SapInternal)
}
