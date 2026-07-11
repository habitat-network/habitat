package login

import (
	"context"
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
		loginHint string,
	) (redirectURI string, state []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires. It returns the login ID of
	// the authenticated identity.
	//
	// oauthState is the OAuth "state" query parameter echoed back on the
	// callback URL; providers that don't use it (e.g. password, google) may
	// ignore it.
	Exchange(
		ctx context.Context,
		code string,
		issuer string,
		oauthState string,
		state []byte,
	) (loginID string, err error)
}
