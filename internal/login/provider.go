package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Provider abstracts a login backend. Each implementation handles a specific
// login method (e.g., "atproto", "google", "password") and is responsible for
// its own credential storage.
type Provider interface {
	// LoginMethod returns a stable identifier used to route authorization
	// requests to the correct provider based on the org's configured method.
	LoginMethod() string

	// Authorize starts the auth flow and returns the redirect URL plus opaque
	// provider-specific state to be stored in the session flash.
	Authorize(ctx context.Context, id *identity.Identity) (redirectUri string, state []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires.
	Exchange(ctx context.Context, did syntax.DID, code string, issuer string, state []byte) error
}

// Router selects the correct Provider for a given login method.
type Router struct {
	providers map[string]Provider
}

func NewRouter(providers ...Provider) *Router {
	r := &Router{providers: make(map[string]Provider, len(providers))}
	for _, p := range providers {
		r.providers[p.LoginMethod()] = p
	}
	return r
}

// ByLoginMethod returns the provider registered for the given login method.
func (r *Router) ByLoginMethod(method string) (Provider, error) {
	p, ok := r.providers[method]
	if !ok {
		return nil, fmt.Errorf("no login provider for method %q", method)
	}
	return p, nil
}
