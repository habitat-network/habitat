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
	// OpensocialStore, if set, is used to add an email-provisioned DID to
	// its org once it completes sign-in (see Exchange and ExchangeEmail).
	OpensocialStore *opensocial.Store
	// EmailProvisioner, if set, mints the identity for a work email that
	// hasn't signed in before, once ExchangeEmail has verified its owner
	// controls it.
	EmailProvisioner EmailProvisioner
}

// EmailProvisioner provisions the identity for a verified work email (see
// identity.EmailResolver.ProvisionEmailIdentity).
type EmailProvisioner interface {
	ProvisionEmailIdentity(ctx context.Context, email emaildomain.Email) (syntax.DID, error)
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

// provisionEmailMember adds did to its org now that it has verified its
// provisioned email via sign-in, minting the org's admin membership on the
// first such sign-in and a plain membership otherwise (see
// opensocial.Store.ProvisionMember). It's a no-op if OpensocialStore isn't
// set.
func (r *LoginRouter) provisionEmailMember(ctx context.Context, did syntax.DID) error {
	if r.OpensocialStore == nil {
		return nil
	}
	orgDID, ok, err := r.EmailStore.GetOrgDID(ctx, did)
	if err != nil {
		return fmt.Errorf("get provisioned org: %w", err)
	}
	if !ok {
		return fmt.Errorf("no org provisioned for %s", did)
	}
	if err := r.OpensocialStore.ProvisionMember(ctx, orgDID, did); err != nil {
		return fmt.Errorf("provision member: %w", err)
	}
	return nil
}

// domainLogin returns the login provider for email's domain, for an email
// that has no identity provisioned yet.
func (r *LoginRouter) domainLogin(
	ctx context.Context,
	email emaildomain.Email,
) (login.Provider, error) {
	if r.EmailStore == nil {
		return nil, fmt.Errorf("email sign-in is not configured")
	}
	_, method, ok, err := r.EmailStore.LookupDomain(ctx, email.Domain())
	if err != nil {
		return nil, fmt.Errorf("lookup email domain: %w", err)
	}
	if !ok {
		return nil, fmt.Errorf("email domain %s is not set up for sign-in", email.Domain())
	}
	provider := r.emailProvider(method)
	if provider == nil {
		return nil, fmt.Errorf("unsupported login provider for %s", email.Domain())
	}
	return provider, nil
}

// AuthorizeEmail begins sign-in for a work email that has no identity yet,
// through its domain's login method. No identity exists until ExchangeEmail
// verifies the email.
func (r *LoginRouter) AuthorizeEmail(
	ctx context.Context,
	email emaildomain.Email,
) (string, []byte, error) {
	provider, err := r.domainLogin(ctx, email)
	if err != nil {
		return "", nil, err
	}
	return provider.Authorize(ctx, string(email))
}

// ExchangeEmail completes sign-in begun by AuthorizeEmail. Only once the
// login provider confirms the user controls email does it provision email's
// identity (minting it if it's still new) and add it to its org. It returns
// the provisioned DID.
func (r *LoginRouter) ExchangeEmail(
	ctx context.Context,
	email emaildomain.Email,
	query url.Values,
	state []byte,
) (syntax.DID, error) {
	if r.EmailProvisioner == nil {
		return "", fmt.Errorf("email sign-in is not configured")
	}
	provider, err := r.domainLogin(ctx, email)
	if err != nil {
		return "", err
	}
	loginID, err := provider.Exchange(ctx, query, state)
	if err != nil {
		return "", fmt.Errorf("failed to exchange code: %w", err)
	}
	if !strings.EqualFold(loginID, string(email)) {
		return "", fmt.Errorf("login id mismatch: %s != %s", email, loginID)
	}
	did, err := r.EmailProvisioner.ProvisionEmailIdentity(ctx, email)
	if err != nil {
		return "", fmt.Errorf("provision email identity: %w", err)
	}
	if err := r.provisionEmailMember(ctx, did); err != nil {
		return "", err
	}
	return did, nil
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
		loginID, err := provider.Exchange(ctx, query, state)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		if !strings.EqualFold(loginID, string(email)) {
			return fmt.Errorf("login id mismatch: %s != %s", email, loginID)
		}
		return r.provisionEmailMember(ctx, did)
	}

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
