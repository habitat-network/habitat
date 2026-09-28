package authn

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type CredentialInfo struct {
	Subject syntax.DID
	Org     org.Org
	Space   habitat_syntax.SpaceURI
	// Method is the auth method that validated the request. It is stamped by
	// EndpointOptions.Validate, which copies the credential a Method returned
	// rather than writing through it: a Method may hold onto the credential it
	// returns and hand the same one to later requests.
	Method ValidatorMethod
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
