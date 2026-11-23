package identity

import (
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
)

// NewDirectory returns the default atproto identity directory, resolving
// did:plc DIDs against plcURL instead of the public plc.directory when it is
// set. Test beds point it at a local PLC server.
func NewDirectory(plcURL string) identity.Directory {
	if plcURL == "" {
		return identity.DefaultDirectory()
	}
	// DefaultDirectory wraps a BaseDirectory in a cache; rebuild the same
	// stack around the PLC URL.
	base := identity.BaseDirectory{
		PLCURL:              plcURL,
		HTTPClient:          http.Client{Timeout: 10 * time.Second},
		TryAuthoritativeDNS: true,
	}
	// Short TTLs: a test bed creates identities and then resolves them at once.
	return identity.NewCacheDirectory(&base, 10_000, time.Minute, 2*time.Second, 2*time.Second)
}
