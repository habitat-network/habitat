package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/opensocial"
)

const (
	// handlePrefixMaxLen leaves room within hive's 50-char handle prefix
	// limit for an 8-char collision suffix.
	handlePrefixMaxLen = 42
	// mintAttempts bounds retries when a generated handle is already taken.
	mintAttempts = 5
)

// EmailResolver resolves a work email to the habitat identity provisioned
// for it, minting one and enrolling it in the email domain's org on first
// sight (see emaildomain.Store.CreateDomainMapping).
type EmailResolver struct {
	db              *gorm.DB
	emailStore      *emaildomain.Store
	hive            hive.Hive
	opensocialStore *opensocial.Store
}

func NewEmailResolver(
	db *gorm.DB,
	emailStore *emaildomain.Store,
	h hive.Hive,
	opensocialStore *opensocial.Store,
) *EmailResolver {
	return &EmailResolver{db: db, emailStore: emailStore, hive: h, opensocialStore: opensocialStore}
}

// ResolveEmailIdentity returns the identity provisioned for email, minting
// and enrolling one (admin if it's the org's first member, member
// otherwise) if email's domain is mapped to an org and email hasn't been
// seen before. It returns identity.ErrDIDNotFound if the domain isn't
// mapped.
func (r *EmailResolver) ResolveEmailIdentity(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, error) {
	if ident, ok, err := r.lookupProvisioned(ctx, email); err != nil || ok {
		return ident, err
	}
	orgDID, _, ok, err := r.emailStore.LookupDomain(ctx, email.Domain())
	if err != nil {
		return nil, fmt.Errorf("lookup email domain: %w", err)
	}
	if !ok {
		return nil, identity.ErrDIDNotFound
	}
	orgIdent, err := r.hive.LookupDID(ctx, orgDID)
	if err != nil {
		return nil, fmt.Errorf("lookup org identity: %w", err)
	}
	// Org handles are a single label minted as "<label>.<memberDomain>";
	// members are minted beneath it, e.g. "alice.acme.<memberDomain>".
	orgLabel, _, _ := strings.Cut(orgIdent.Handle.String(), ".")

	var minted *identity.Identity
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ident, err := mintMemberIdentity(ctx, r.hive.WithTx(tx), email, orgLabel)
		if err != nil {
			return err
		}
		if err := r.opensocialStore.WithTx(tx).ProvisionMember(ctx, orgDID, ident.DID); err != nil {
			return err
		}
		if err := r.emailStore.WithTx(tx).Provision(ctx, email, orgDID, ident.DID); err != nil {
			return err
		}
		minted = ident
		return nil
	})
	if errors.Is(err, emaildomain.ErrEmailProvisioned) {
		// A concurrent first sign-in for the same email won; ours rolled
		// back, so return theirs.
		ident, ok, err := r.lookupProvisioned(ctx, email)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("email %s provisioned concurrently but not found", email)
		}
		return ident, nil
	}
	if err != nil {
		return nil, fmt.Errorf("provision %s: %w", email, err)
	}
	return minted, nil
}

// lookupProvisioned returns the identity email was already provisioned as,
// if any.
func (r *EmailResolver) lookupProvisioned(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, bool, error) {
	did, ok, err := r.emailStore.GetDID(ctx, email)
	if err != nil {
		return nil, false, fmt.Errorf("get provisioned did: %w", err)
	}
	if !ok {
		return nil, false, nil
	}
	ident, err := r.hive.LookupDID(ctx, did)
	if err != nil {
		return nil, false, fmt.Errorf("lookup provisioned identity: %w", err)
	}
	return ident, true, nil
}

// mintMemberIdentity mints an identity under orgLabel whose handle is
// derived from email's local part, adding a random suffix if that's taken.
func mintMemberIdentity(
	ctx context.Context,
	h hive.Hive,
	email emaildomain.Email,
	orgLabel string,
) (*identity.Identity, error) {
	base := handlePrefix(email.LocalPart())
	candidate := base
	for range mintAttempts {
		ident, err := h.MintIdentity(ctx, candidate, orgLabel)
		if err == nil {
			return ident, nil
		}
		if !errors.Is(err, hive.ErrNotCreated) {
			return nil, fmt.Errorf("mint member identity: %w", err)
		}
		suffix := make([]byte, 4)
		if _, err := rand.Read(suffix); err != nil {
			return nil, fmt.Errorf("generate handle suffix: %w", err)
		}
		candidate = base + hex.EncodeToString(suffix)
	}
	return nil, fmt.Errorf(
		"mint member identity for %s: handle taken after %d attempts",
		email,
		mintAttempts,
	)
}

// handlePrefix keeps only the characters hive allows in a handle prefix
// ([a-z0-9], since email is already lowercased), falling back to "member"
// when none remain.
func handlePrefix(localPart string) string {
	var b strings.Builder
	for _, c := range localPart {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
		if b.Len() == handlePrefixMaxLen {
			break
		}
	}
	if b.Len() == 0 {
		return "member"
	}
	return b.String()
}
