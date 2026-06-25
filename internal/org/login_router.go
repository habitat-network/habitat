package org

import (
	"context"
	"fmt"

	"github.com/habitat-network/habitat/internal/login"
)

// LoginRouter dispatches login flows to the provider configured for an org's
// login method. It has no knowledge of org/member state: callers resolve the
// target org (e.g. via Store.GetOrgForDID) and verify the provider-asserted
// login ID returned by Exchange against the expected member or admin.
type LoginRouter struct {
	Pds      login.Provider
	Google   login.Provider
	Password login.Provider
}

func (r *LoginRouter) getProvider(org Org) login.Provider {
	switch org.loginMethod(context.Background() /* todo: fix context */) {
	case LoginMethodGoogle:
		return r.Google
	case LoginMethodPassword:
		return r.Password
	case LoginMethodAtproto:
		return r.Pds
	}
	return nil
}

// Authorize starts a login flow for targetOrg. loginHint identifies which
// member's credential is expected; pass "" for an org-DID login, where any
// admin may complete the flow.
func (r *LoginRouter) Authorize(
	ctx context.Context,
	targetOrg Org,
	loginHint string,
) (string, []byte, error) {
	provider := r.getProvider(targetOrg)
	if provider == nil {
		return "", nil, fmt.Errorf("unsupported login provider for org %s", targetOrg.DID())
	}
	return provider.Authorize(ctx, loginHint)
}

// Exchange completes a login flow for targetOrg and returns the
// provider-asserted login ID for the caller to verify against the expected
// member or admin.
func (r *LoginRouter) Exchange(
	ctx context.Context,
	targetOrg Org,
	code string,
	issuer string,
	state []byte,
) (loginID string, err error) {
	provider := r.getProvider(targetOrg)
	if provider == nil {
		return "", fmt.Errorf("unsupported login provider for org %s", targetOrg.DID())
	}
	return provider.Exchange(ctx, code, issuer, state)
}
