package org

import (
	"context"
	"errors"
	"fmt"
	"net/url"

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
	switch org.LoginMethod(context.Background() /* todo: fix context */) {
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

func (r *LoginRouter) Exchange(
	ctx context.Context,
	did syntax.DID,
	query url.Values,
	state []byte,
) error {
	// org login (requires admin)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err == nil {
		provider := r.getProvider(fetchedOrg)
		if provider == nil {
			return fmt.Errorf("unsupported login provider for %s", did)
		}
		loginID, err := provider.Exchange(ctx, query, state)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		member, err := r.OrgStore.GetMemberByLoginID(ctx, loginID)
		if err != nil {
			return fmt.Errorf("failed to get member by login id: %w", err)
		}
		if member.Role != AdminRole {
			return fmt.Errorf("not an admin")
		}
		return nil
	} else if !errors.Is(err, ErrOrgNotFound) {
		return fmt.Errorf("failed to get org: %w", err)
	}

	// member login
	member, err := r.OrgStore.GetMember(ctx, did)
	if err != nil {
		return fmt.Errorf("failed to get member: %w", err)
	}
	provider := r.getProvider(member.Org)
	if provider == nil {
		return fmt.Errorf("unsupported login provider for %s", did)
	}
	loginID, err := provider.Exchange(ctx, query, state)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}
	if member.LoginID != loginID {
		return fmt.Errorf("login id mismatch: %s != %s", member.LoginID, loginID)
	}
	return nil
}
