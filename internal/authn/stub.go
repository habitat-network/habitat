package authn

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/core"
)

// stubOrg implements core.Org for tests.
type stubOrg struct {
	did                syntax.DID
	loginMethod        core.LoginMethod
	getMetadataHandler func(context.Context, string) habitat.NetworkHabitatOrgGetMetadataOutput
}

func (s *stubOrg) DID() syntax.DID { return s.did }

func (s *stubOrg) LoginMethod(_ context.Context) core.LoginMethod {
	if s.loginMethod != "" {
		return s.loginMethod
	}
	return core.LoginMethodAtproto
}

func (s *stubOrg) GetMetadata(
	ctx context.Context,
	domain string,
) habitat.NetworkHabitatOrgGetMetadataOutput {
	if s.getMetadataHandler != nil {
		return s.getMetadataHandler(ctx, domain)
	}
	return habitat.NetworkHabitatOrgGetMetadataOutput{}
}

func (s *stubOrg) AddAdmin(_ context.Context, _ syntax.DID) error         { return nil }
func (s *stubOrg) GetAdmins(_ context.Context) ([]syntax.DID, error)      { return nil, nil }
func (s *stubOrg) GetMembers(_ context.Context) ([]syntax.DID, error)     { return nil, nil }
func (s *stubOrg) RemoveAdmin(_ context.Context, _ syntax.DID) error      { return nil }
func (s *stubOrg) RemoveMembers(_ context.Context, _ []syntax.DID) error  { return nil }
func (s *stubOrg) DowngradeAdmin(_ context.Context, _ syntax.DID) error   { return nil }
func (s *stubOrg) IsAdmin(_ context.Context, _ syntax.DID) (bool, error)  { return false, nil }
func (s *stubOrg) IsMember(_ context.Context, _ syntax.DID) (bool, error) { return false, nil }
func (s *stubOrg) WithTx(_ *gorm.DB) core.Org                             { return s }

// stubAuthn implements authn.Method for tests, always returning the given DID.
type stubAuthn struct {
	did      syntax.DID
	credType CredentialType
	org      core.Org
}

func (s *stubAuthn) CanHandle(_ *http.Request) bool { return true }

func (s *stubAuthn) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*CredentialInfo, bool) {
	return &CredentialInfo{Subject: s.did, Type: s.credType, Org: s.org}, true
}

func (s *stubAuthn) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (*CredentialInfo, bool, error) {
	return &CredentialInfo{Subject: s.did, Type: s.credType, Org: s.org}, true, nil
}

func NewStubAuthnForTest(did syntax.DID) Method {
	return &stubAuthn{
		did:      did,
		credType: UserCredential,
	}
}

func NewStubAuthnForTestWithOrg(did, orgDID syntax.DID) Method {
	return &stubAuthn{
		did:      did,
		credType: UserCredential,
		org:      &stubOrg{did: orgDID},
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
