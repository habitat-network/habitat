package org

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/opensocial"
)

const (
	// orgHandleMaxLen leaves room within an org handle's 50-char limit for an
	// 8-char collision suffix.
	orgHandleMaxLen = 42
	// newOrgAttempts bounds retries when a generated org handle is taken.
	newOrgAttempts = 5
)

type LoginRouter struct {
	Pds    login.Provider
	Google login.Provider
	// WorkOS signs in members of orgs created with a WorkOS organization
	// (see emaildomain.LoginMethodWorkOS).
	WorkOS   login.Provider
	Password login.Provider
	OrgStore Store
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
	case emaildomain.LoginMethodWorkOS:
		return r.WorkOS
	}
	return nil
}

// emailLogin returns the login provider and provisioned email for did if it
// was provisioned via email-domain sign-in; ok is false for any other DID.
func (r *LoginRouter) emailLogin(
	ctx context.Context,
	did syntax.DID,
) (login.Provider, emaildomain.LoginMethod, emaildomain.Email, bool, error) {
	if r.EmailStore == nil {
		return nil, "", "", false, nil
	}
	method, ok, err := r.EmailStore.GetLoginMethod(ctx, did)
	if err != nil {
		return nil, "", "", false, fmt.Errorf("get email login method: %w", err)
	}
	if !ok {
		return nil, "", "", false, nil
	}
	email, ok, err := r.EmailStore.GetEmail(ctx, did)
	if err != nil {
		return nil, "", "", false, fmt.Errorf("get provisioned email: %w", err)
	}
	if !ok {
		return nil, "", "", false, fmt.Errorf("no email provisioned for %s", did)
	}
	provider := r.emailProvider(method)
	if provider == nil {
		return nil, "", "", false, fmt.Errorf("unsupported login provider for %s", did)
	}
	return provider, method, email, true, nil
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

// placeWorkOSMember decides which org did joins once WorkOS has verified its
// email, and records it (see emaildomain.Store.SetMemberOrg).
//   - did already belongs to a domain-mapped org: WorkOS must report a
//     WorkOS organization mapped to that org, else sign-in is rejected.
//   - did has no org yet: it joins the org mapped to the first of its WorkOS
//     organizations that has one. If none is mapped, the first WorkOS
//     organization gets a new org (and mapping), or, for a user in no WorkOS
//     organization, a new personal org named after the email.
func (r *LoginRouter) placeWorkOSMember(
	ctx context.Context,
	did syntax.DID,
	email emaildomain.Email,
	externalOrgs []login.ExternalOrg,
) error {
	orgDID, ok, err := r.EmailStore.GetOrgDID(ctx, did)
	if err != nil {
		return fmt.Errorf("get provisioned org: %w", err)
	}
	if !ok {
		return fmt.Errorf("no org provisioned for %s", did)
	}
	ids := make([]string, len(externalOrgs))
	for i, o := range externalOrgs {
		ids[i] = o.ID
	}
	if orgDID != "" {
		// A personal org (no WorkOS organization behind it) admits its
		// owner on their verified email alone.
		mapped, err := r.EmailStore.OrgHasWorkOSOrg(ctx, orgDID)
		if err != nil {
			return err
		}
		if !mapped {
			return nil
		}
		member, err := r.EmailStore.HasWorkOSOrg(ctx, orgDID, ids)
		if err != nil {
			return err
		}
		if !member {
			return fmt.Errorf("not a member of the workos organization for %s", orgDID)
		}
		return nil
	}
	if r.OpensocialStore == nil {
		return fmt.Errorf("no opensocial store to place %s in an org", did)
	}

	orgDID, ok, err = r.EmailStore.LookupWorkOSOrg(ctx, ids)
	if err != nil {
		return err
	}
	if !ok {
		name := email.LocalPart()
		if len(externalOrgs) > 0 {
			name = externalOrgs[0].Name
		}
		orgDID, err = r.newOrg(ctx, name)
		if err != nil {
			return err
		}
		if len(externalOrgs) > 0 {
			err := r.EmailStore.CreateWorkOSOrgMapping(ctx, externalOrgs[0].ID, orgDID)
			if err != nil {
				return fmt.Errorf("map workos org: %w", err)
			}
		}
	}
	if err := r.EmailStore.SetMemberOrg(ctx, did, orgDID); err != nil {
		return err
	}
	return nil
}

// newOrg creates an empty org (its first member becomes admin, see
// opensocial.Store.ProvisionMember) whose handle is derived from name, with a
// random suffix if that handle is taken.
func (r *LoginRouter) newOrg(ctx context.Context, name string) (syntax.DID, error) {
	var b strings.Builder
	for _, c := range strings.ToLower(name) {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
		if b.Len() == orgHandleMaxLen {
			break
		}
	}
	base := b.String()
	if base == "" {
		base = "org"
	}
	candidate := base
	for range newOrgAttempts {
		orgDID, err := r.OpensocialStore.NewOrgWithoutCreator(ctx, candidate)
		if err == nil {
			return syntax.DID(orgDID), nil
		}
		if !errors.Is(err, hive.ErrNotCreated) {
			return "", fmt.Errorf("create org: %w", err)
		}
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			return "", fmt.Errorf("generate handle suffix: %w", err)
		}
		candidate = base + hex.EncodeToString(suffix)
	}
	return "", fmt.Errorf("create org %q: handle taken after %d attempts", base, newOrgAttempts)
}

func (r *LoginRouter) Authorize(
	ctx context.Context,
	did syntax.DID,
) (string, string, []byte, error) {
	// email-domain member login
	if provider, _, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return "", "", nil, err
	} else if ok {
		return provider.Authorize(ctx, string(email))
	}

	// org login (requires admin credential)
	fetchedOrg, err := r.OrgStore.GetOrg(ctx, did)
	if err == nil {
		provider := r.getProvider(fetchedOrg)
		if provider == nil {
			return "", "", nil, fmt.Errorf("unsupported login provider for %s", did)
		}
		return provider.Authorize(ctx, "" /* loginHint (empty because any admin will work) */)
	} else if !errors.Is(err, ErrOrgNotFound) {
		return "", "", nil, fmt.Errorf("failed to get org: %w", err)
	}

	// member login
	member, err := r.OrgStore.GetMember(ctx, did)
	if err != nil {
		return "", "", nil, fmt.Errorf("failed to get member: %w", err)
	}
	provider := r.getProvider(member.Org)
	if provider == nil {
		return "", "", nil, fmt.Errorf("unsupported login provider for %s", did)
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
	if provider, method, email, ok, err := r.emailLogin(ctx, did); err != nil {
		return err
	} else if ok {
		loginID, profile, err := provider.Exchange(ctx, query, state)
		if err != nil {
			return fmt.Errorf("failed to exchange code: %w", err)
		}
		if !strings.EqualFold(loginID, string(email)) {
			return fmt.Errorf("login id mismatch: %s != %s", email, loginID)
		}
		if method == emaildomain.LoginMethodWorkOS {
			if err := r.placeWorkOSMember(ctx, did, email, profile.ExternalOrgs); err != nil {
				return err
			}
		}
		if err := r.provisionEmailMember(ctx, did); err != nil {
			return err
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
	orgDID, ok, err := r.EmailStore.GetOrgDID(ctx, did)
	if err != nil {
		return fmt.Errorf("get provisioned org: %w", err)
	}
	if !ok || orgDID == "" {
		return nil
	}
	if err := r.OpensocialStore.SeedMemberProfile(
		ctx, orgDID, did, profile.Name, profile.Picture,
	); err != nil {
		return fmt.Errorf("seed member profile: %w", err)
	}
	return nil
}
