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
	// Method names the auth method that produced this credential. Each Method
	// sets it on the credential it returns, so provenance holds however the
	// Method was reached: through EndpointOptions.Validate, or called
	// directly — which RawMethod callers necessarily do, having no request to
	// route through the validator.
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
