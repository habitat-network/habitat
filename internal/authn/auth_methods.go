package authn

import (
	"context"
	"net/http"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/org"
)

type CredentialType int

const (
	OrgCredential CredentialType = iota
	UserCredential
)

type CredentialInfo struct {
	Subject syntax.DID
	Org     org.Org
	Type    CredentialType
}

type Method interface {
	CanHandle(r *http.Request) bool
	Validate(w http.ResponseWriter, r *http.Request, scopes ...string) (*CredentialInfo, bool)
}

type RawMethod interface {
	ValidateRaw(ctx context.Context, token string, scopes ...string) (*CredentialInfo, bool, error)
}

type Validator struct {
	authMethods          []Method
	supportedCredentials []CredentialType
}

type ValidatorOption func(*Validator)

func NewValidator(options ...ValidatorOption) *Validator {
	v := &Validator{}
	for _, option := range options {
		option(v)
	}
	return v
}

func WithAuthMethods(authMethods ...Method) ValidatorOption {
	return func(v *Validator) {
		v.authMethods = authMethods
	}
}

func WithSupportedCredentials(supportedCredentials ...CredentialType) ValidatorOption {
	return func(v *Validator) {
		v.supportedCredentials = supportedCredentials
	}
}

func (v *Validator) Validate(w http.ResponseWriter, r *http.Request) (*CredentialInfo, bool) {
	for _, method := range v.authMethods {
		if method.CanHandle(r) {
			credInfo, ok := method.Validate(w, r)
			if !ok {
				return nil, false
			}
			if len(v.supportedCredentials) > 0 &&
				!slices.Contains(v.supportedCredentials, credInfo.Type) {
				return nil, false
			}
			return credInfo, true
		}
	}
	w.WriteHeader(http.StatusUnauthorized)
	return nil, false
}
