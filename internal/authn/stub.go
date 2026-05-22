package authn

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// stubAuthn implements authn.Method for tests, always returning the given DID.
type stubAuthn struct {
	did syntax.DID
}

func (s *stubAuthn) CanHandle(_ *http.Request) bool { return true }

func (s *stubAuthn) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (syntax.DID, bool) {
	return s.did, true
}

func (s *stubAuthn) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (syntax.DID, bool, error) {
	return s.did, true, nil
}

func NewStubAuthnForTest(did syntax.DID) Method {
	return &stubAuthn{
		did: did,
	}
}

// stubAuthnFailed implements authn.Method for tests, always failing auth.
type stubAuthnFailed struct{}

func (s *stubAuthnFailed) CanHandle(_ *http.Request) bool { return true }

func (s *stubAuthnFailed) Validate(
	w http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (syntax.DID, bool) {
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return "", false
}

func (s *stubAuthnFailed) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (syntax.DID, bool, error) {
	return "", false, nil
}

func NewStubAuthnFailedForTest() Method {
	return &stubAuthnFailed{}
}
