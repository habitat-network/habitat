package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
)

// Provider abstracts a login backend. Each implementation handles a specific
// login method (e.g., "atproto", "google", "password") and is responsible for
// its own credential storage.
type Provider interface {
	// LoginMethod returns a stable identifier used to route authorization
	// requests to the correct provider based on the org's configured method.
	LoginMethod() org.LoginMethod

	// Authorize starts the auth flow and returns the redirect URL plus opaque
	// provider-specific state to be stored in the session flash.
	// loginID is the provider-specific identifier stored on the Member
	// (e.g. password hash, public ATProto DID, google email).
	Authorize(
		ctx context.Context,
		id *identity.Identity,
		loginID string,
	) (redirectUri string, state []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires.
	Exchange(ctx context.Context, did syntax.DID, code string, issuer string, state []byte) error
}

// Router selects the correct Provider for a given login method.
type Router struct {
	providers map[org.LoginMethod]Provider
}

func NewRouter(providers ...Provider) *Router {
	r := &Router{providers: make(map[org.LoginMethod]Provider, len(providers))}
	for _, p := range providers {
		r.providers[p.LoginMethod()] = p
	}
	return r
}

// ByLoginMethod returns the provider registered for the given login method.
func (r *Router) ByLoginMethod(method org.LoginMethod) (Provider, error) {
	p, ok := r.providers[method]
	if !ok {
		return nil, fmt.Errorf("no login provider for method %q", method)
	}
	return p, nil
}
