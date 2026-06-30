package org

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/login"
)

// LoginRouter dispatches login flows to the provider configured for an org's
// login method, and resolves/verifies the org or member behind a DID via
// OrgStore.
type LoginRouter struct {
	Pds      login.Provider
	Google   login.Provider
	Password login.Provider
	OrgStore Store
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

// Authorize starts a login flow for did. If did is an org's own DID, the
// flow accepts any admin's credential (empty login hint); otherwise did must
// be a registered member, and the flow expects that member's own credential.
func (r *LoginRouter) Authorize(
	ctx context.Context,
	did syntax.DID,
) (string, []byte, error) {
	// org login (requires admin credential)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err == nil {
		provider := r.getProvider(fetchedOrg)
		if provider == nil {
			return "", nil, fmt.Errorf("unsupported login provider for %s", did)
		}
		return provider.Authorize(ctx, "" /* loginHint (empty because any admin will work) */)
	} else if !errors.Is(err, ErrOrgNotFound) {
		return "", nil, fmt.Errorf("failed to get org: %w", err)
	}

	// member login
	member, err := r.OrgStore.GetMember(ctx, did)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get member: %w", err)
	}
	provider := r.getProvider(member.Org)
	if provider == nil {
		return "", nil, fmt.Errorf("unsupported login provider for %s", did)
	}
	return provider.Authorize(ctx, member.LoginID)
}

// Exchange completes a login flow for did and returns the member who
// completed it: did's own member entry for a member-DID login, or the
// authenticating admin for an org-DID login. Callers can compare the
// returned member's DID against did to tell which case occurred.
func (r *LoginRouter) Exchange(
	ctx context.Context,
	did syntax.DID,
	code string,
	issuer string,
	state []byte,
) (*Member, error) {
	// org login (requires admin)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err == nil {
		provider := r.getProvider(fetchedOrg)
		if provider == nil {
			return nil, fmt.Errorf("unsupported login provider for %s", did)
		}
		loginID, err := provider.Exchange(ctx, code, issuer, state)
		if err != nil {
			return nil, fmt.Errorf("failed to exchange code: %w", err)
		}
		member, err := r.OrgStore.GetMemberByLoginID(ctx, loginID)
		if err != nil {
			return nil, fmt.Errorf("failed to get member by login id: %w", err)
		}
		if member.Role != AdminRole {
			return nil, fmt.Errorf("not an admin")
		}
		return member, nil
	} else if !errors.Is(err, ErrOrgNotFound) {
		return nil, fmt.Errorf("failed to get org: %w", err)
	}

	// member login
	member, err := r.OrgStore.GetMember(ctx, did)
	if err != nil {
		return nil, fmt.Errorf("failed to get member: %w", err)
	}
	provider := r.getProvider(member.Org)
	if provider == nil {
		return nil, fmt.Errorf("unsupported login provider for %s", did)
	}
	loginID, err := provider.Exchange(ctx, code, issuer, state)
	if err != nil {
		return nil, fmt.Errorf("failed to exchange code: %w", err)
	}
	if member.LoginID != loginID {
		return nil, fmt.Errorf("login id mismatch: %s != %s", member.LoginID, loginID)
	}
	return member, nil
}
