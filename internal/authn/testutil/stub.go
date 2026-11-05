package testutil

import (
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/authn"
)

// success implements authn.Method for tests, always returning the given DID.
type success struct {
	did syntax.DID
}

func (s *success) CanHandle(_ *http.Request) bool { return true }

func (s *success) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*authn.CredentialInfo, bool) {
	return &authn.CredentialInfo{Subject: s.did}, true
}

func NewSuccessMethod(did syntax.DID) authn.Method {
	return &success{
		did: did,
	}
}

// failure implements authn.Method for tests, always failing auth.
type failure struct{}

func (s *failure) CanHandle(_ *http.Request) bool { return true }

func (s *failure) Validate(
	w http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*authn.CredentialInfo, bool) {
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return nil, false
}

func NewFailMethod() authn.Method {
	return &failure{}
}

type cantHandle struct{}

// CanHandle implements [authn.Method].
func (c *cantHandle) CanHandle(r *http.Request) bool {
	return false
}

// Validate implements [authn.Method].
func (c *cantHandle) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*authn.CredentialInfo, bool) {
	return nil, false
}

func NewCantHandleMethod() authn.Method {
	return &cantHandle{}
}
