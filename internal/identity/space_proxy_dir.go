package identity

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/emaildomain"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/utils"
)

// SpaceProxyDirectory resolves identities through a base directory and returns
// each one with its DID document overridden so its #atproto_pds service points
// at this habitat instance — unless the identity's real PDS already implements
// the atproto spaces protocol. It also resolves work emails already
// provisioned to an identity when an EmailResolver is configured.
type SpaceProxyDirectory struct {
	base          identity.Directory
	emailResolver *EmailResolver
	domain        string
	httpClient    *http.Client
}

// NewSpaceProxyDirectory constructs an SpaceProxyDirectory over base, redirecting
// identities whose real PDS doesn't support spaces to serve from this habitat
// instance.
func NewSpaceProxyDirectory(
	base identity.Directory,
	domain string,
	opts ...utils.Opt[SpaceProxyDirectory],
) *SpaceProxyDirectory {
	dir := utils.ResolveOptions(SpaceProxyDirectory{
		base:       base,
		domain:     domain,
		httpClient: httpx.NewClient(),
	}, opts)
	return &dir
}

// WithClient sets the HTTP client used to probe whether an identity's PDS
// supports the atproto spaces protocol.
func WithClient(client *http.Client) utils.Opt[SpaceProxyDirectory] {
	return func(d *SpaceProxyDirectory) {
		d.httpClient = client
	}
}

// WithEmailResolver lets the directory resolve a work email in place of a
// handle (see EmailResolver). Resolution never mints an identity. Without it,
// email identifiers are rejected.
func WithEmailResolver(r *EmailResolver) utils.Opt[SpaceProxyDirectory] {
	return func(d *SpaceProxyDirectory) {
		d.emailResolver = r
	}
}

// LookupDID implements identity.Directory.
func (d *SpaceProxyDirectory) LookupDID(
	ctx context.Context,
	did syntax.DID,
) (*identity.Identity, error) {
	ident, err := d.base.LookupDID(ctx, did)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// LookupHandle implements identity.Directory.
func (d *SpaceProxyDirectory) LookupHandle(
	ctx context.Context,
	handle syntax.Handle,
) (*identity.Identity, error) {
	ident, err := d.base.LookupHandle(ctx, handle)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// Lookup implements identity.Directory.
func (d *SpaceProxyDirectory) Lookup(
	ctx context.Context,
	atid syntax.AtIdentifier,
) (*identity.Identity, error) {
	ident, err := d.base.Lookup(ctx, atid)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// Purge implements identity.Directory.
func (d *SpaceProxyDirectory) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	return d.base.Purge(ctx, atid)
}

// LookupEmail resolves a work email to the identity already provisioned for
// it and applies the DID override to it. It returns identity.ErrInvalidHandle
// when no EmailResolver is configured, emaildomain.ErrEmailNotProvisioned
// when the email's domain is mapped but it hasn't completed sign-in yet, and
// identity.ErrDIDNotFound when the email's domain isn't mapped.
func (d *SpaceProxyDirectory) LookupEmail(
	ctx context.Context,
	email emaildomain.Email,
) (*identity.Identity, error) {
	if d.emailResolver == nil {
		return nil, identity.ErrInvalidHandle
	}
	ident, err := d.emailResolver.ResolveEmailIdentity(ctx, email)
	if err != nil {
		return nil, err
	}
	return d.applyOverride(ctx, ident), nil
}

// LookupIdentifier resolves an identifier string as an at-identifier (DID or
// handle) or, failing that, as a work email. It returns
// identity.ErrInvalidHandle for input that is neither, or for a valid email
// when no EmailResolver is configured; identity.ErrHandleNotFound for an
// at-identifier the base directory can't resolve; and identity.ErrDIDNotFound
// (or emaildomain.ErrEmailNotProvisioned, which wraps it) for a valid email
// with no provisioned identity (surfaced from LookupEmail).
func (d *SpaceProxyDirectory) LookupIdentifier(
	ctx context.Context,
	identifier string,
) (*identity.Identity, error) {
	if atid, err := syntax.ParseAtIdentifier(identifier); err == nil {
		return d.Lookup(ctx, atid)
	}
	if email, err := emaildomain.ParseEmail(identifier); err == nil {
		return d.LookupEmail(ctx, email)
	}
	return nil, identity.ErrInvalidHandle
}

// applyOverride returns ident, or a copy of it whose #atproto_pds service
// points at this habitat instance when the identity's real PDS doesn't support
// the spaces protocol. A fresh identity is returned rather than mutating the
// base directory's — possibly cached — one.
func (d *SpaceProxyDirectory) applyOverride(
	ctx context.Context,
	ident *identity.Identity,
) *identity.Identity {
	if utils.SupportsSpaces(ctx, d.httpClient, ident) {
		return ident
	}
	keys := make(map[string]identity.VerificationMethod, len(ident.Keys))
	for k, v := range ident.Keys {
		keys[k] = v
	}
	return &identity.Identity{
		DID:         ident.DID,
		Handle:      ident.Handle,
		AlsoKnownAs: append([]string(nil), ident.AlsoKnownAs...),
		Keys:        keys,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				Type: "AtprotoPersonalDataServer",
				URL:  "https://" + d.domain,
			},
		},
	}
}
