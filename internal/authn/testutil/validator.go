package testutil

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
)

type validatorImpl struct {
	success        bool
	credentialInfo *authn.CredentialInfo
}

// Validate implements [authn.Validator].
func (s *validatorImpl) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*authn.CredentialInfo, bool) {
	if !s.success {
		httpx.WriteUnauthorized(r.Context(), w, "")
		return nil, false
	}
	return s.credentialInfo, true
}

// Request implements [authn.RequestValidator].
func (s *validatorImpl) Request(options ...authn.ValidatorOption) authn.Validator {
	return s
}

func NewSuccessValidator(credentialInfo *authn.CredentialInfo) authn.RequestValidator {
	return &validatorImpl{credentialInfo: credentialInfo, success: true}
}

// NewSuccessValidatorWithOrg returns a validator that authenticates the given
// DID as a member of the given org, mirroring NewSuccessMethodWithOrg.
func NewSuccessValidatorWithOrg(did, orgDID syntax.DID) authn.RequestValidator {
	return NewSuccessValidator(&authn.CredentialInfo{
		Subject: did,
		Type:    authn.UserCredential,
		Org:     &stubOrg{did: orgDID},
	})
}

func NewFailureValidator() authn.RequestValidator {
	return &validatorImpl{success: false}
}
