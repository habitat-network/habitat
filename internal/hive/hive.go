package hive

import (
	"context"
	"regexp"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"gorm.io/gorm"
)

var handlePattern = regexp.MustCompile(`^[a-zA-Z0-9]{1,50}$`)

// hive roughly stands for "habitat identity verification and enrollment" and it is an identity
// service for habitat organizations.
// This package serves org identities based on the DID spec and the did:web: method to be legible to and interoperable
// with the broader AT protocol network, but allow orgs to own the user identities and management and
// not rely on the PLC directory as a central source of failure.

type Hive interface {
	// Minting new identities for members
	MintIdentity(handle string) error

	// FUTURE METHODS:
	// Updating a handle
	// UpdateHandle(ctx context.Context, did string, oldHandle string, newHandle string)
	// Rotate signing key
	// Deactivate user

	// Implements the same interface as the PLC / any identity Directory in atproto land
	identity.Directory
}

type hive struct {
	// TODO: these can't really be changed after the first member is minted; should they be stored in the database for more permanence rather than runtime args?

	// The sub-domain on which members are minted / served for this hive
	// e.g in "alice.sf.club", this would be "sf.club"
	domain string
	// member namespace (subdomain of the domain, or "" if none)
	// "members" in alice.members.sf.club and "" in alice.sf.club
	memberNamespace string

	// The backing data store for hive
	store *store
}

var _ Hive = &hive{}

// baseDomain returns the full host suffix used in both DIDs and handles:
// "<memberNamespace>.<domain>" or just "<domain>" if memberNamespace is empty.
func (h *hive) baseDomain() string {
	if h.memberNamespace != "" {
		return h.memberNamespace + "." + h.domain
	}
	return h.domain
}

// toIdentity builds an identity.Identity from a stored IdentPublic and its known DID.
func (h *hive) toIdentity(pub IdentPublic) *identity.Identity {
	baseDomain := h.baseDomain()
	fullHandle := syntax.Handle(pub.Handle + "." + baseDomain)
	fullDID := syntax.DID("did:web:" + pub.OpaqueID + "." + baseDomain)
	return &identity.Identity{
		DID:         fullDID,
		Handle:      fullHandle,
		AlsoKnownAs: []string{"at://" + string(fullHandle)},
		Keys: map[string]identity.VerificationMethod{
			"atproto": {
				Type:               "Multikey",
				PublicKeyMultibase: pub.SigningPublicKey,
			},
		},
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {
				Type: "AtprotoPersonalDataServer",
				URL:  "https://" + baseDomain,
			},
		},
	}
}

func NewHive(domain string, memberNamespace string, db *gorm.DB) (Hive, error) {
	store, err := newStore(db)
	if err != nil {
		return nil, err
	}
	return &hive{
		domain:          domain,
		memberNamespace: memberNamespace,
		store:           store,
	}, nil
}

// Lookup implements identity.Directory
func (h *hive) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	if atid.IsDID() {
		did, err := atid.AsDID()
		if err != nil {
			return nil, err
		}
		return h.LookupDID(ctx, did)
	}
	handle, err := atid.AsHandle()
	if err != nil {
		return nil, err
	}
	return h.LookupHandle(ctx, handle)
}

// LookupDID implements identity.Directory
func (h *hive) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	// DID format: did:web:<opaqueID>.<baseDomain>
	const webPrefix = "did:web:"
	didStr := did.String()
	if !strings.HasPrefix(didStr, webPrefix) {
		return nil, identity.ErrDIDNotFound
	}
	host := didStr[len(webPrefix):]

	suffix := "." + h.baseDomain()
	if !strings.HasSuffix(host, suffix) {
		return nil, identity.ErrDIDNotFound
	}

	opaqueID := host[:len(host)-len(suffix)]
	pub, err := h.store.getIdentityByID(opaqueID)
	if err != nil {
		return nil, err
	}

	return h.toIdentity(pub), nil
}

// LookupHandle implements identity.Directory
// It strips the internal handle prefix from the given handle which has format
// <internal-handle>.membersNamespace.domain or <internal-handle>.domain if membersNamespace == ""
// and looks up the handle against the store.
func (h *hive) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	suffix := "." + h.baseDomain()
	handleStr := string(handle)
	if !strings.HasSuffix(handleStr, suffix) {
		return nil, identity.ErrHandleNotFound
	}

	internalHandle := handleStr[:len(handleStr)-len(suffix)]
	pub, err := h.store.getIdentityByHandle(internalHandle)
	if err != nil {
		return nil, err
	}

	return h.toIdentity(pub), nil
}

// Purge implements identity.Directory
func (h *hive) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	// Empty because we're not caching anything
	return nil
}

// MintOrgMember implements Hive.
// TODO: we need either a password or piggy back on some other credential here
// For now (development), no password, return true from oauth server always
func (h *hive) MintIdentity(handle string) error {
	// Ensure handle passes regex
	if !handlePattern.MatchString(handle) {
		return identity.ErrInvalidHandle
	}
	return h.store.createIdentity(handle)
}
