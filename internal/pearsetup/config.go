// Package pearsetup assembles a complete Pear instance from a plain
// configuration value. It exists so that both cmd/pear and tests can build the
// same fully wired server; cmd/pear owns flag parsing, this package owns
// everything after it.
package pearsetup

import (
	"errors"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"gocloud.dev/blob"

	"github.com/habitat-network/habitat/internal/fgastore"
)

// Config is everything New needs to build a Pear. Fields are plain values that
// the caller has already parsed; nothing here knows about CLI flags.
type Config struct {
	// Domain is the publicly reachable domain this server is served from.
	Domain string
	// HiveDomain is the domain member identities are minted under. Defaults to Domain.
	HiveDomain string
	// DB is the database DSN, in the form internal/db accepts.
	DB string
	// Port is the TCP port to listen on. Defaults to "8000".
	Port string
	// HTTPSCerts is a directory holding fullchain.pem and privkey.pem. Empty serves plain HTTP.
	HTTPSCerts string
	// Debug turns on request logging and stdout logs.
	Debug bool

	// OAuthServerSecret is the parsed 32-byte secret backing the OAuth server,
	// its cookie store, and the password login provider.
	OAuthServerSecret []byte
	// PDSCredEncryptKey is the parsed key encrypting stored PDS credentials.
	PDSCredEncryptKey []byte
	// OAuthClientSecret is the secret this server uses as an OAuth *client* of a user's PDS.
	OAuthClientSecret string
	// PDSOAuthClientURI is the client identity URI presented to a PDS. A bare
	// host is prefixed with https://; empty falls back to Domain.
	PDSOAuthClientURI string
	// SpaceSigningKey signs space commits for repo owners on external PDSes.
	SpaceSigningKey atcrypto.PrivateKey
	// AdminPasswordHash is the argon2id hash of the instance admin password.
	AdminPasswordHash string

	// GoogleClientID and GoogleClientSecret enable the Google login provider.
	// The provider is registered only when both are set.
	GoogleClientID     string
	GoogleClientSecret string

	// UIDevProxy proxies /ui/ to a dev server instead of serving embedded assets.
	UIDevProxy string
	// BuiltinApps are the client IDs allowed to use the JWT bearer grant.
	BuiltinApps []string
	// BlobBucket is the gocloud.dev blob URL for blob storage. Ignored when Bucket is set.
	BlobBucket string

	// Directory resolves atproto identities that hive does not host. Defaults
	// to identity.DefaultDirectory(), which resolves over the network — tests
	// must override it to stay offline.
	Directory identity.Directory
	// FGA overrides the relationship store. Defaults to a store chosen from the
	// DB dialect: Postgres shares the main DSN, SQLite gets a sibling file.
	FGA fgastore.Store
	// Bucket overrides blob storage. Defaults to opening BlobBucket.
	Bucket *blob.Bucket

	// DisableP2P skips the libp2p host and its catch-all route. Named
	// negatively so the zero value is production behavior.
	DisableP2P bool
	// DisableUI skips the /ui/ handler.
	DisableUI bool
}

func (c Config) withDefaults() Config {
	if c.HiveDomain == "" {
		c.HiveDomain = c.Domain
	}
	if c.PDSOAuthClientURI == "" {
		c.PDSOAuthClientURI = c.Domain
	}
	if !strings.HasPrefix(c.PDSOAuthClientURI, "https://") {
		c.PDSOAuthClientURI = "https://" + c.PDSOAuthClientURI
	}
	if c.Port == "" {
		c.Port = "8000"
	}
	if c.Directory == nil {
		c.Directory = identity.DefaultDirectory()
	}
	return c
}

func (c Config) validate() error {
	if c.Domain == "" {
		return errors.New("domain is required")
	}
	if c.DB == "" {
		return errors.New("db DSN is required")
	}
	if c.SpaceSigningKey == nil {
		return errors.New("space signing key is required")
	}
	if len(c.OAuthServerSecret) == 0 {
		return errors.New("oauth server secret is required")
	}
	if len(c.PDSCredEncryptKey) == 0 {
		return errors.New("pds cred encrypt key is required")
	}
	return nil
}
