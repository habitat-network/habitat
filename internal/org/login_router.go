package org

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/opensocial"
)

type LoginRouter struct {
	Pds      login.Provider
	Google   login.Provider
	Password login.Provider
	OrgStore Store
	// EmailStore, if set, routes DIDs provisioned via email-domain sign-in
	// (see identity.EmailResolver) through their domain's login method.
	EmailStore *emaildomain.Store
	// OpensocialStore, if set alongside EmailStore, seeds a first-time
	// email-domain member's profile from the login provider's account
	// metadata (e.g. Google name/picture) once Exchange verifies it.
	OpensocialStore *opensocial.Store
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

// emailProvider returns the login provider for an email domain's login
// method, or nil if it isn't configured on this instance.
func (r *LoginRouter) emailProvider(method emaildomain.LoginMethod) login.Provider {
	switch method {
	case emaildomain.LoginMethodGoogle:
		return r.Google
	}
	return nil
}

// emailLogin returns the login provider and provisioned email for did if it
// was provisioned via email-domain sign-in; ok is false for any other DID.
func (r *LoginRouter) emailLogin(
	ctx context.Context,
	did syntax.DID,
) (login.Provider, emaildomain.Email, bool, error) {
	if r.EmailStore == nil {
		return nil, "", false, nil
	}
	method, ok, err := r.EmailStore.GetLoginMethod(ctx, did)
	if err != nil {
		return nil, "", false, fmt.Errorf("get email login method: %w", err)
	}
	if !ok {
		return nil, "", false, nil
	}
	email, ok, err := r.EmailStore.GetEmail(ctx, did)
	if err != nil {
		return nil, "", false, fmt.Errorf("get provisioned email: %w", err)
	}
	if !ok {
		return nil, "", false, fmt.Errorf("no email provisioned for %s", did)
	}
	provider := r.emailProvider(method)
	if provider == nil {
		return nil, "", false, fmt.Errorf("unsupported login provider for %s", did)
	}
	return provider, email, true, nil
}

func (r *LoginRouter) Authorize(
	ctx context.Context,
	did syntax.DID,
) (string, []byte, error) {
	// email-domain member login
	if provider, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return "", nil, err
	} else if ok {
		return provider.Authorize(ctx, string(email))
	}

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
	// email-domain member login
	if provider, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return err
	} else if ok {
		loginID, profile, err := provider.Exchange(ctx, query, state)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		if !strings.EqualFold(loginID, string(email)) {
			return fmt.Errorf("login id mismatch: %s != %s", email, loginID)
		}
		if err := r.seedMemberProfile(ctx, did, email, profile); err != nil {
			return fmt.Errorf("failed to seed member profile: %w", err)
		}
		return nil
	}

	// org login (requires admin)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err == nil {
		provider := r.getProvider(fetchedOrg)
		if provider == nil {
			return fmt.Errorf("unsupported login provider for %s", did)
		}
		loginID, _, err := provider.Exchange(ctx, query, state)
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
	loginID, _, err := provider.Exchange(ctx, query, state)
	if err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}
	if member.LoginID != loginID {
		return fmt.Errorf("login id mismatch: %s != %s", member.LoginID, loginID)
	}
	return nil
}

// seedMemberProfile best-effort seeds did's community.opensocial.memberProfile
// record from profile once its email-domain sign-in has been verified by
// Exchange, if the org has such a store configured and profile carries
// anything usable. It never overwrites a profile the member has since
// customized (see opensocial.Store.SeedMemberProfile).
func (r *LoginRouter) seedMemberProfile(
	ctx context.Context,
	did syntax.DID,
	email emaildomain.Email,
	profile login.Profile,
) error {
	if r.OpensocialStore == nil || (profile.Name == "" && profile.Picture == "") {
		return nil
	}
	orgDID, _, ok, err := r.EmailStore.LookupDomain(ctx, email.Domain())
	if err != nil {
		return fmt.Errorf("lookup email domain: %w", err)
	}
	if !ok {
		return nil
	}
	if err := r.OpensocialStore.SeedMemberProfile(
		ctx, orgDID, did, profile.Name, profile.Picture,
	); err != nil {
		return fmt.Errorf("seed member profile: %w", err)
	}
	return nil
}
