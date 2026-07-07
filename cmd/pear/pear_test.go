package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

// TestStartup boots the full pear server via run() against a throwaway SQLite
// DB and polls the /health endpoint, exercising the real startup wiring (DB,
// FGA, oauth server, stores, router). It's a smoke test: it catches setup
// regressions (e.g. a mis-constructed FGA path) that unit tests don't.
func TestStartup(t *testing.T) {
	port := freePort(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// The secret flags are base64-decoded by encrypt.ParseKey, so pass the
	// encoded form of the 32-byte test key rather than the raw bytes.
	key := base64.StdEncoding.EncodeToString(encrypt.TestKey)
	cmd := &cli.Command{Flags: getFlags(), Action: run}
	args := []string{
		"pear",
		"--domain=localhost",
		"--db=sqlite://" + dbPath,
		"--port=" + port,
		"--pds_cred_encrypt_key=" + key,
		"--oauth_server_secret=" + key,
		"--oauth_client_secret=" + key,
		"--admin_password=password",
	}

	// run() blocks until ctx is cancelled; drive it on a goroutine and surface
	// any startup error via the channel so a failed boot fails the test.
	errCh := make(chan error, 1)
	go func() { errCh <- cmd.Run(ctx, args) }()

	require.Eventually(t, func() bool {
		select {
		case err := <-errCh:
			require.NoError(t, err, "server exited before becoming healthy")
		default:
		}
		resp, err := http.Get(fmt.Sprintf("http://localhost:%s/health", port))
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 30*time.Second, 100*time.Millisecond, "server never became healthy")

	cancel()
	<-errCh
}

// freePort reserves an ephemeral port and returns it, so the test server binds
// somewhere unused rather than a hardcoded port that could collide.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)
	return port
}
