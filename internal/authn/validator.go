package authn

import (
	"fmt"
	"net/http"

	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/httpx"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

type ValidatorMethod int

const (
	ValidatorMethodOAuth ValidatorMethod = iota
	ValidatorMethodServiceAuth
	ValidatorMethodSpaceCredential
	ValidatorMethodDelegationToken
)

type RequestValidator interface {
	Request(options ...utils.Opt[EndpointOptions]) Validator
}

type validator struct {
	oauth           Method
	serviceAuth     *AtprotoServiceAuthMethod
	spaceCredential *SpaceCredentialAuthMethod
	delegationToken *DelegationTokenAuthMethod
	fga             fgastore.Store
}

func NewValidator(
	oauth Method,
	serviceAuth *AtprotoServiceAuthMethod,
	spaceCredential *SpaceCredentialAuthMethod,
	delegationToken *DelegationTokenAuthMethod,
	fga fgastore.Store,
) *validator {
	return &validator{
		oauth:           oauth,
		serviceAuth:     serviceAuth,
		spaceCredential: spaceCredential,
		delegationToken: delegationToken,
		fga:             fga,
	}
}

func (v *validator) Request(
	options ...utils.Opt[EndpointOptions],
) Validator {
	r := utils.ResolveOptions(options...)
	r.v = v
	return &r
}

func WithMethods(authMethods ...ValidatorMethod) utils.Opt[EndpointOptions] {
	return func(rv *EndpointOptions) {
		rv.authMethods = authMethods
	}
}

func WithSpace(space habitat_syntax.SpaceURI, relation string) utils.Opt[EndpointOptions] {
	return func(rv *EndpointOptions) {
		rv.space = space
		rv.relation = relation
	}
}

type EndpointOptions struct {
	v           *validator
	authMethods []ValidatorMethod
	space       habitat_syntax.SpaceURI
	relation    string
}

func (rv *EndpointOptions) getMethod(method ValidatorMethod) Method {
	switch method {
	case ValidatorMethodOAuth:
		return rv.v.oauth
	case ValidatorMethodServiceAuth:
		return rv.v.serviceAuth
	case ValidatorMethodSpaceCredential:
		return rv.v.spaceCredential
	case ValidatorMethodDelegationToken:
		return rv.v.delegationToken
	}
	return nil
}

func (rv *EndpointOptions) Validate(
	w http.ResponseWriter,
	r *http.Request,
	scopes ...string,
) (*CredentialInfo, bool) {
	ctx := r.Context()
	for _, vm := range rv.authMethods {
		m := rv.getMethod(vm)
		if !m.CanHandle(r) {
			continue
		}
		credInfo, ok := m.Validate(w, r)
		if !ok {
			return nil, false
		}
		if rv.space != "" {
			if credInfo.Space != "" {
				if rv.relation != fgastore.RelationSpaceReader {
					httpx.WriteUnauthorized(ctx, w, "space token can only read")
					return nil, false
				}
				if credInfo.Space != rv.space {
					httpx.WriteUnauthorized(ctx, w, "space credential mismatch")
					return nil, false
				}
			} else if credInfo.Subject != "" {
				authz, err := rv.v.fga.Check(
					ctx,
					fgastore.MemberUserString(credInfo.Subject),
					rv.relation,
					fgastore.SpaceObjectKey(rv.space),
					fgastore.Tuple{
						User:     fgastore.MemberUserString(rv.space.SpaceOwner()),
						Relation: fgastore.RelationSpaceOwner,
						Object:   fgastore.SpaceObjectKey(rv.space),
					},
					fgastore.OrgMemberContextualTuple(credInfo.Org.DID()),
				)
				if err != nil {
					httpx.WriteServerError(ctx, w, fmt.Errorf("check membership: %w", err))
					return nil, false
				}
				if !authz {
					httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("not a member"))
					return nil, false
				}
			}
		}
		return credInfo, true
	}
	httpx.WriteUnauthorized(ctx, w, "no supported auth method")
	return nil, false
}
