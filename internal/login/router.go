package login

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/opensocial"
)

type Router struct {
	Pds    Provider
	Google Provider
	// Passthrough, if set along with Directory, signs in DIDs whose handle
	// ends in PassthroughHandleSuffix without any authentication. Dev/test only.
	Passthrough Provider
	// Directory resolves a DID to its handle for the Passthrough check.
	Directory identity.Directory
	// EmailStore, if set, routes DIDs provisioned via email-domain sign-in
	// (see identity.EmailResolver) through their domain's login method.
	EmailStore *emaildomain.Store
	// OpensocialStore, if set, is used to add an email-provisioned DID to
	// its org once it completes sign-in (see Exchange). identity.
	// EmailResolver mints such a DID without joining it to the org, so a
	// mistyped or unowned email never becomes a ghost member. It also seeds
	// the new member's profile from the login provider's account metadata
	// (e.g. Google name/picture).
	OpensocialStore *opensocial.Store
}

// emailProvider returns the login provider for an email domain's login
// method, or nil if it isn't configured on this instance.
func (r *Router) emailProvider(method emaildomain.LoginMethod) Provider {
	switch method {
	case emaildomain.LoginMethodGoogle:
		return r.Google
	}
	return nil
}

// emailLogin returns the login provider and provisioned email for did if it
// was provisioned via email-domain sign-in; ok is false for any other DID.
func (r *Router) emailLogin(
	ctx context.Context,
	did syntax.DID,
) (Provider, emaildomain.Email, bool, error) {
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
func (r *Router) provisionEmailMember(ctx context.Context, did syntax.DID) error {
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

// passthroughLogin reports whether did should skip authentication: it's a
// did:web:*.test DID, or its handle ends in PassthroughHandleSuffix.
func (r *Router) passthroughLogin(ctx context.Context, did syntax.DID) bool {
	if r.Passthrough == nil {
		return false
	}
	if strings.HasPrefix(string(did), "did:web:") &&
		strings.HasSuffix(string(did), PassthroughHandleSuffix) {
		return true
	}
	if r.Directory == nil {
		return false
	}
	ident, err := r.Directory.LookupDID(ctx, did)
	if err != nil {
		return false
	}
	return strings.HasSuffix(string(ident.Handle), PassthroughHandleSuffix)
}

func (r *Router) Authorize(
	ctx context.Context,
	did syntax.DID,
) (string, string, string, error) {
	if r.passthroughLogin(ctx, did) {
		return r.Passthrough.Authorize(ctx, did.String())
	}
	// email-domain member login
	if provider, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return "", "", "", err
	} else if ok {
		return provider.Authorize(ctx, string(email))
	}
	// Everything else signs in through its PDS.
	return r.Pds.Authorize(ctx, did.String())
}

func (r *Router) Exchange(
	ctx context.Context,
	did syntax.DID,
	query url.Values,
	providerStateKey string,
) error {
	if r.passthroughLogin(ctx, did) {
		if _, _, err := r.Passthrough.Exchange(ctx, query, providerStateKey); err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		return nil
	}
	// email-domain member login
	if provider, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return err
	} else if ok {
		loginID, profile, err := provider.Exchange(ctx, query, providerStateKey)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		if !strings.EqualFold(loginID, string(email)) {
			return fmt.Errorf("login id mismatch: %s != %s", email, loginID)
		}
		if err := r.provisionEmailMember(ctx, did); err != nil {
			return err
		}
		if err := r.seedMemberProfile(ctx, did, email, profile); err != nil {
			return fmt.Errorf("failed to seed member profile: %w", err)
		}
		return nil
	}
	// Everything else signs in through its PDS.
	if _, _, err := r.Pds.Exchange(ctx, query, providerStateKey); err != nil {
		return fmt.Errorf("failed to exchange code: %w", err)
	}
	return nil
}

// seedMemberProfile best-effort seeds did's community.opensocial.memberProfile
// record from profile once its email-domain sign-in has been verified by
// Exchange, if the org has such a store configured and profile carries
// anything usable. It never overwrites a profile the member has since
// customized (see opensocial.Store.SeedMemberProfile).
func (r *Router) seedMemberProfile(
	ctx context.Context,
	did syntax.DID,
	email emaildomain.Email,
	profile Profile,
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
