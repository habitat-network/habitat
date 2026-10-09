package login

import (
	"context"
	"net/url"
)

// Profile carries account metadata a login provider learned while completing
// Exchange (e.g. Google's account name/picture). Used to seed a first-time
// member's community.opensocial.memberProfile record. The zero value means
// the provider has nothing to offer.
type Profile struct {
	Name    string
	Picture string
	// ExternalOrgs are the identity provider's own organizations the user
	// belongs to (e.g. WorkOS organizations). Empty for providers without
	// such a concept.
	ExternalOrgs []ExternalOrg
}

// ExternalOrg is an organization in a login provider's own directory.
type ExternalOrg struct {
	ID   string
	Name string
}

// Provider abstracts a login backend. Each implementation handles a specific
// login method (e.g., "atproto", "google", "password") and is responsible for
// its own credential storage.
type Provider interface {
	// Authorize starts the auth flow and returns the redirect URL, the OAuth
	// `state` the callback will echo back (empty if the provider has none),
	// plus opaque provider-specific state to be stored in the session flash.
	// The echoed state lets callers tell concurrent, nested flows apart.
	// loginID is the provider-specific identifier stored on the Member
	// (e.g. password hash, public ATProto DID, google email).
	Authorize(
		ctx context.Context,
		loginHint string,
	) (redirectURI string, state string, providerState []byte, err error)

	// Exchange exchanges the callback code for credentials and should persist
	// whatever credentials the provider acquires. The query parameters from the
	// OAuth callback URL are passed so each provider can extract what it needs
	// (e.g. "code", "iss"). profile carries whatever account metadata the
	// provider learned along the way; providers with nothing to offer return
	// the zero value.
	Exchange(
		ctx context.Context,
		query url.Values,
		state []byte,
	) (loginId string, profile Profile, err error)
}
