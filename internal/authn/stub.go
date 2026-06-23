package authn

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// stubAuthn implements authn.Method for tests, always returning the given DID.
type stubAuthn struct {
	did      syntax.DID
	credType CredentialType
}

func (s *stubAuthn) CanHandle(_ *http.Request) bool { return true }

func (s *stubAuthn) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*CredentialInfo, bool) {
	return &CredentialInfo{Subject: s.did, Type: s.credType}, true
}

func (s *stubAuthn) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (*CredentialInfo, bool, error) {
	return &CredentialInfo{Subject: s.did, Type: s.credType}, true, nil
}

func NewStubAuthnForTest(did syntax.DID) Method {
	return &stubAuthn{
		did:      did,
		credType: UserCredential,
	}
}

// stubAuthnFailed implements authn.Method for tests, always failing auth.
type stubAuthnFailed struct{}

func (s *stubAuthnFailed) CanHandle(_ *http.Request) bool { return true }

func (s *stubAuthnFailed) Validate(
	w http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*CredentialInfo, bool) {
	http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
	return nil, false
}

func (s *stubAuthnFailed) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (*CredentialInfo, bool, error) {
	return nil, false, nil
}

func NewStubAuthnFailedForTest() Method {
	return &stubAuthnFailed{}
}
