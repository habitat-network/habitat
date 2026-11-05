package testutil

import (
	"net/http"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
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
		httpx.WriteUnauthorized(r.Context(), w, "", nil)
		return nil, false
	}
	return s.credentialInfo, true
}

// Request implements [authn.RequestValidator].
func (s *validatorImpl) Request(options ...utils.Opt[authn.EndpointOptions]) authn.Validator {
	return s
}

func NewSuccessValidator(credentialInfo *authn.CredentialInfo) authn.RequestValidator {
	return &validatorImpl{credentialInfo: credentialInfo, success: true}
}

func NewFailureValidator() authn.RequestValidator {
	return &validatorImpl{success: false}
}
