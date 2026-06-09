package org

import (
	"context"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/login"
)

type LoginRouter struct {
	Pds      login.Provider
	Google   login.Provider
	Password login.Provider
	OrgStore Store
}

func (r *LoginRouter) getProvider(org Org) login.Provider {
	switch org.loginMethod() {
	case LoginMethodGoogle:
		return r.Google
	case LoginMethodPassword:
		return r.Password
	case LoginMethodAtproto:
		return r.Pds
	}
	return nil
}

func (r *LoginRouter) Authorize(
	ctx context.Context,
	did syntax.DID,
) (string, []byte, error) {
	member, err := r.OrgStore.GetMember(ctx, did)
	// member login
	if err == nil {
		provider := r.getProvider(member.Org)
		if provider == nil {
			return "", nil, fmt.Errorf("unsupported login provider for %s", did)
		}
		return provider.Authorize(ctx, member.LoginID)
	} else if !errors.Is(err, ErrMemberNotFound) {
		return "", nil, fmt.Errorf("failed to get member: %w", err)
	}
	// org login (requires admin credential)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err != nil {
		return "", nil, fmt.Errorf("failed to get org: %w", err)
	}
	provider := r.getProvider(fetchedOrg)
	if provider == nil {
		return "", nil, fmt.Errorf("unsupported login provider for %s", did)
	}
	return provider.Authorize(ctx, "" /* loginHint (empty because any admin will work) */)
}

func (r *LoginRouter) Exchange(
	ctx context.Context,
	did syntax.DID,
	code string,
	issuer string,
	state []byte,
) error {
	member, err := r.OrgStore.GetMember(ctx, did)
	// member login
	if err == nil {
		provider := r.getProvider(member.Org)
		if provider == nil {
			return fmt.Errorf("unsupported login provider for %s", did)
		}
		loginID, err := provider.Exchange(ctx, code, issuer, state)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		if member.LoginID != loginID {
			return fmt.Errorf("login id mismatch: %s != %s", member.LoginID, loginID)
		}
		return nil
	}
	// org login (requires admin)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err != nil {
		return fmt.Errorf("failed to get org: %w", err)
	}
	provider := r.getProvider(fetchedOrg)
	if provider == nil {
		return fmt.Errorf("unsupported login provider for %s", did)
	}
	loginID, err := provider.Exchange(ctx, code, issuer, state)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}
	member, err = r.OrgStore.GetMemberByLoginID(ctx, loginID)
	if err != nil {
		return fmt.Errorf("failed to get member by login id: %w", err)
	}
	if member.Role != AdminRole {
		return fmt.Errorf("not an admin")
	}
	return nil
}
