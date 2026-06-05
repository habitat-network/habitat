package org

import (
	"context"
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

func (r *LoginRouter) GetProvider(org Org) login.Provider {
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
	if err != nil {
		return "", nil, err
	}
	provider := r.GetProvider(member.Org)
	if provider == nil {
		return "", nil, fmt.Errorf("no login provider for %s", did)
	}
	return provider.Authorize(ctx, did, member.LoginID)
}

func (r *LoginRouter) Exchange(
	ctx context.Context,
	did syntax.DID,
	code string,
	issuer string,
	state []byte,
) error {
	member, err := r.OrgStore.GetMember(ctx, did)
	if err != nil {
		return err
	}
	provider := r.GetProvider(member.Org)
	if provider == nil {
		return fmt.Errorf("no login provider for %s", did)
	}
	return provider.Exchange(ctx, did, member.LoginID, code, issuer, state)
}
