// Package testutil stands up a complete, real Pear instance for tests: real
// stores, real routes, real authentication. Server tests use it instead of
// hand-assembling components behind a stub validator, so the authorization
// checks their handlers declare are actually exercised.
package testutil

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/base64"
	"path/filepath"
	"testing"

	"github.com/alexedwards/argon2id"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/stretchr/testify/require"
	"gocloud.dev/blob/memblob"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/pearsetup"
	"github.com/habitat-network/habitat/internal/utils"
)

// Domain is the domain every harness instance is served from. Tests that build
// URIs or set a Host header should use it rather than hardcoding a string.
const Domain = "pear.test"

// TestPear is a running Pear instance. It embeds *pearsetup.Pear, so every
// store and server is reachable for seeding fixtures directly, while the
// request helpers drive the same routes production serves.
type TestPear struct {
	*pearsetup.Pear

	T               *testing.T
	OAuthSigningKey *ecdsa.PrivateKey
}

// WithDirectory replaces the identity directory used for DIDs hive does not
// host. The default resolves nothing, which keeps tests offline; supply a
// populated identity.MockDirectory when a test needs an external DID.
func WithDirectory(dir identity.Directory) utils.Opt[pearsetup.Config] {
	return func(c *pearsetup.Config) { c.Directory = dir }
}

// WithFGA replaces the relationship store.
func WithFGA(store fgastore.Store) utils.Opt[pearsetup.Config] {
	return func(c *pearsetup.Config) { c.FGA = store }
}

// WithConfig is the escape hatch for settings without a dedicated option.
func WithConfig(fn func(*pearsetup.Config)) utils.Opt[pearsetup.Config] {
	return fn
}

// New builds a Pear backed by a temporary SQLite database, an in-memory
// relationship store, and in-memory blob storage, with the libp2p host and the
// UI handler switched off. Everything is torn down when the test ends.
func New(t *testing.T, opts ...utils.Opt[pearsetup.Config]) *TestPear {
	t.Helper()

	hostKey, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)

	fga, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err)
	t.Cleanup(func() { _ = fga.Close() })

	passwordHash, err := argon2id.CreateHash("admin", argon2id.DefaultParams)
	require.NoError(t, err)

	oauthServerKey := randomKey(t)

	oauthServerSigningKey, err := ecdsa.ParseRawPrivateKey(oauthServerKey)
	require.NoError(t, err)

	cfg := pearsetup.Config{
		Domain:            Domain,
		DB:                "sqlite://" + filepath.Join(t.TempDir(), "test.db"),
		OAuthServerSecret: oauthServerKey,
		PDSCredEncryptKey: randomKey(t),
		OAuthClientSecret: randomEncryptionKey(t),
		SpaceSigningKey:   hostKey,
		AdminPasswordHash: passwordHash,
		Directory:         identity.NewMockDirectory(),
		FGA:               fga,
		Bucket:            memblob.OpenBucket(nil),
		DisableP2P:        true,
		DisableUI:         true,
	}
	for _, opt := range opts {
		opt(&cfg)
	}

	p, err := pearsetup.New(t.Context(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	return &TestPear{Pear: p, T: t, OAuthSigningKey: oauthServerSigningKey}
}

// randomKey returns 32 random bytes. The OAuth server parses its secret as a
// P-256 scalar, for which any 32 random bytes are overwhelmingly likely to be
// valid.
func randomKey(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, 32)
	_, err := rand.Read(b)
	require.NoError(t, err)
	return b
}

// randomEncryptionKey returns a base64-encoded 32-byte key, the format
// internal/encrypt.ParseKey requires. The PDS OAuth client secret is parsed
// through this same path, and — like OAuthServerSecret — as an ECDSA P-256
// scalar, for which random bytes are overwhelmingly likely to be valid.
func randomEncryptionKey(t *testing.T) string {
	t.Helper()
	return base64.StdEncoding.EncodeToString(randomKey(t))
}
