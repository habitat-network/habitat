package authn

import (
	"context"
	"net/http"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type CredentialInfo struct {
	Subject syntax.DID
	Space   habitat_syntax.SpaceURI
	// Audience is the verified Atproto-Space-Audience of a key-bound space
	// credential request; empty for unbound credentials.
	Audience string
	// Method names the auth method that produced this credential. Each Method
	// sets it on the credential it returns, so provenance holds however the
	// Method was reached: through EndpointOptions.Validate, or called
	// directly — which RawMethod callers necessarily do, having no request to
	// route through the validator.
	Method ValidatorMethod
	// ExpiresAt is when a long-lived request opened with this credential, such
	// as a subscribe stream, must end; zero when it needn't. Service auth
	// leaves it zero: its tokens are minted per request and expire within a
	// minute or so, so, as with relays' subscriptions, one authorizes a stream
	// when it opens.
	ExpiresAt time.Time
	// Scopes are the OAuth scopes granted to the token, for OAuth
	// credentials; nil for every other method.
	Scopes []string
	// Authorizer is the member who approved an OAuth session acting as a
	// group; empty for every other credential. A group's access through such
	// a session is narrowed by the authorizer's current roles.
	Authorizer syntax.DID
}

type Validator interface {
	Validate(w http.ResponseWriter, r *http.Request, scopes ...string) (*CredentialInfo, bool)
}

type Method interface {
	Validator
	CanHandle(r *http.Request) bool
}

type RawMethod interface {
	ValidateRaw(ctx context.Context, token string, scopes ...string) (*CredentialInfo, bool, error)
}
