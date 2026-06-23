package authn

import (
	"context"
	"fmt"
	"net/http"
	"slices"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type CredentialType int

const (
	InstanceCredential CredentialType = iota
	OrgCredential
	UserCredential
)

type CredentialInfo struct {
	Subject syntax.DID
	Type    CredentialType
}

type Method interface {
	CanHandle(r *http.Request) bool
	Validate(w http.ResponseWriter, r *http.Request, scopes ...string) (*CredentialInfo, bool)
	ValidateRaw(ctx context.Context, token string, scopes ...string) (*CredentialInfo, bool, error)
}

type Validator struct {
	authMethods          []Method
	supportedCredentials []CredentialType
}

func NewValidator(authMethods ...Method) *Validator {
	return &Validator{authMethods: authMethods}
}

func (v *Validator) WithSupportedCredentials(supportedCredentials ...CredentialType) *Validator {
	v.supportedCredentials = supportedCredentials
	return v
}

func (v *Validator) WithRequiredSubject() *Validator {
	v.supportedCredentials = []CredentialType{UserCredential, OrgCredential}
	return v
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

func (v *Validator) ValidateRaw(
	ctx context.Context,
	token string,
	/* TODO: take in scopes here */
) (*CredentialInfo, bool, error) {
	var err error
	var did *CredentialInfo
	var ok bool
	for _, method := range v.authMethods {
		did, ok, err = method.ValidateRaw(ctx, token /* TODO: scopes */)
		// Allow first pass through
		if ok && err == nil {
			return did, true, nil
		}
	}
	// Return the last error
	return did, ok, fmt.Errorf("no auth method passed; latest err: %w", err)
}
