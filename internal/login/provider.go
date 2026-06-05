package login

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// Provider abstracts a login backend. Each implementation handles a specific
// login method (e.g., "atproto", "google", "password") and is responsible for
// its own credential storage.
type Provider interface {
	// Authorize starts the auth flow and returns the redirect URL plus opaque
	// provider-specific state to be stored in the session flash.
	// loginID is the provider-specific identifier stored on the Member
	// (e.g. password hash, public ATProto DID, google email).
	Authorize(
		ctx context.Context,
		did syntax.DID,
		loginID string,
	) (redirectUri string, state []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires.
	Exchange(
		ctx context.Context,
		did syntax.DID,
		loginId string,
		code string,
		issuer string,
		state []byte,
	) error
}
