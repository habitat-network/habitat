package login

import (
	"context"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

type ProviderType = string

const (
	ProviderTypePDS     ProviderType = "pds"
	ProviderTypeHabitat ProviderType = "habitat"
)

// Provider abstracts a login backend. Each implementation handles a specific
// login type (ex: PDS, habitat-org-password, Identity provider / SSO) and is responsible for its own credential storage.
type Provider interface {
	// Type returns a stable identifier stored in the session flash so
	// HandleCallback can route to the correct provider without re-resolving the DID.
	Type() ProviderType

	// CanHandle returns true if this provider handles the given identity.
	CanHandle(id *identity.Identity) bool

	// Authorize starts the auth flow and returns the redirect URL plus opaque
	// provider-specific state to be stored in the session flash.
	Authorize(ctx context.Context, id *identity.Identity) (redirectUri string, state []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires.
	Exchange(ctx context.Context, did syntax.DID, code string, issuer string, state []byte) error
}

// Router selects the correct Provider for a given identity.
type Router struct {
	providers []Provider
}

func NewRouter(providers ...Provider) *Router {
	return &Router{providers: providers}
}

// For returns the first provider whose CanHandle returns true.
// It is assumed that only one provider matches the given identity (for now)
func (r *Router) For(id *identity.Identity) (Provider, error) {
	for _, p := range r.providers {
		if p.CanHandle(id) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no login provider for identity %s", id.DID)
}

// ByType returns the provider registered for the given ProviderType.
func (r *Router) ByType(t ProviderType) (Provider, error) {
	for _, p := range r.providers {
		if p.Type() == t {
			return p, nil
		}
	}
	return nil, fmt.Errorf("no login provider of type %q", t)
}
