package pearsetup_test

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/pearsetup"
)

// randomEncryptionKey returns a base64-encoded 32-byte key, the format
// internal/encrypt.ParseKey requires. Both the OAuth server secret and the
// PDS OAuth client secret are parsed as ECDSA scalars through this same path.
func randomEncryptionKey(t *testing.T) string {
	t.Helper()
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return base64.StdEncoding.EncodeToString(b)
}

// newConfig returns a Config that builds a complete instance without touching
// the network: SQLite on disk, in-memory FGA, in-memory blobs, a mock
// directory, and no libp2p host or UI assets.
func newConfig(t *testing.T) pearsetup.Config {
	t.Helper()

	key, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	secret := make([]byte, 32)
	_, err = rand.Read(secret)
	require.NoError(t, err)
	credKey := make([]byte, 32)
	_, err = rand.Read(credKey)
	require.NoError(t, err)

	return pearsetup.Config{
		Domain:            "pear.test",
		DB:                "sqlite://" + filepath.Join(t.TempDir(), "test.db"),
		OAuthServerSecret: secret,
		PDSCredEncryptKey: credKey,
		OAuthClientSecret: randomEncryptionKey(t),
		SpaceSigningKey:   key,
		AdminPasswordHash: "$argon2id$v=19$m=65536,t=1,p=2$c29tZXNhbHQ$0000000000000000000000000000000000000000000",
		Directory:         identity.NewMockDirectory(),
		FGA:               fga,
		Bucket:            memblob.OpenBucket(nil),
		DisableP2P:        true,
		DisableUI:         true,
	}
}

func TestNewWiresComponents(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	require.NotNil(t, p.DB)
	require.NotNil(t, p.Hive)
	require.NotNil(t, p.OrgStore)
	require.NotNil(t, p.SpacesStore)
	require.NotNil(t, p.PermStore)
	require.NotNil(t, p.OAuthServer)
	require.NotNil(t, p.Validator)
}

func TestNewRejectsIncompleteConfig(t *testing.T) {
	cfg := newConfig(t)
	cfg.Domain = ""

	_, err := pearsetup.New(t.Context(), cfg)
	require.ErrorContains(t, err, "domain")
}

func TestHealthEndpoint(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	req.Host = "pear.test"
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "ok", rec.Body.String())
}

func TestUnroutedPathIs404WithP2PDisabled(t *testing.T) {
	p, err := pearsetup.New(t.Context(), newConfig(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	req := httptest.NewRequest(http.MethodGet, "/no/such/path", http.NoBody)
	req.Host = "pear.test"
	rec := httptest.NewRecorder()
	p.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusNotFound, rec.Code)
}
