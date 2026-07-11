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

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
	tc "github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/urfave/cli/v3"
)

func TestStartup_Sqlite(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	requireStartupHealthy(t, "sqlite://"+dbPath)
}

func TestStartup_Postgres(t *testing.T) {
	ctx := context.Background()

	container, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("pear"),
		postgres.WithUsername("pear"),
		postgres.WithPassword("pear"),
		tc.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
	)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Terminate(ctx) })

	connStr, err := container.ConnectionString(ctx, "sslmode=disable")
	require.NoError(t, err)

	requireStartupHealthy(t, connStr)
}

// requireStartupHealthy boots the pear server against the given database DSN and
// asserts it comes up and serves /health, failing if it exits early. It drives
// the shared startup path so both the SQLite and Postgres tests exercise the
// same boot sequence, differing only in the backing store.
func requireStartupHealthy(t *testing.T, dbDSN string) {
	t.Helper()
	port := freePort(t)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// The secret flags are base64-decoded by encrypt.ParseKey, so pass the
	// encoded form of the 32-byte test key rather than the raw bytes.
	key := base64.StdEncoding.EncodeToString(encrypt.TestKey)
	signingKey, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	cmd := &cli.Command{Flags: getFlags(), Action: run}
	args := []string{
		"pear",
		"--domain=localhost",
		"--db=" + dbDSN,
		"--port=" + port,
		"--pds_cred_encrypt_key=" + key,
		"--oauth_server_secret=" + key,
		"--oauth_client_secret=" + key,
		"--space_signing_key=" + signingKey.Multibase(),
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
		require.NoError(t, resp.Body.Close())
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
	require.NoError(t, l.Close())
	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)
	return port
}
