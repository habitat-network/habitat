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

	// DPoPJKT is the RFC 7638 JWK thumbprint of the key a validated
	// delegation token's DPoP proof was bound to. Only set by
	// [DelegationTokenAuthMethod], for minting a space credential's
	// `cnf.jkt` claim against that same key.
	DPoPJKT string
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
