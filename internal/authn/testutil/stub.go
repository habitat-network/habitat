package testutil

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
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

// success implements authn.Method for tests, always returning the given DID.
type success struct {
	did      syntax.DID
	credType authn.CredentialType
	org      core.Org
}

func (s *success) CanHandle(_ *http.Request) bool { return true }

func (s *success) Validate(
	_ http.ResponseWriter,
	_ *http.Request,
	_ ...string,
) (*authn.CredentialInfo, bool) {
	return &authn.CredentialInfo{Subject: s.did, Type: s.credType, Org: s.org}, true
}

func (s *success) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (*authn.CredentialInfo, bool, error) {
	return &authn.CredentialInfo{Subject: s.did, Type: s.credType, Org: s.org}, true, nil
}

func NewSuccessMethod(did syntax.DID) authn.Method {
	return &success{
		did:      did,
		credType: authn.UserCredential,
	}
}

func NewSuccessMethodWithOrg(did, orgDID syntax.DID) authn.Method {
	return &success{
		did:      did,
		credType: authn.UserCredential,
		org:      &stubOrg{did: orgDID},
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

func (s *failure) ValidateRaw(
	_ context.Context,
	_ string,
	_ ...string,
) (*authn.CredentialInfo, bool, error) {
	return nil, false, nil
}

func NewFailMethod() authn.Method {
	return &failure{}
}
