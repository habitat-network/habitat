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
	MintIdentity(handle string, subdomain string) (*identity.Identity, func(*gorm.DB) error, error)
	// FUTURE METHODS:
	// Updating a handle
	// UpdateHandle(ctx context.Context, did string, oldHandle string, newHandle string)
	// Rotate signing key
	// Deactivate user

	// Implements the same interface as the PLC / any identity Directory in atproto land
	identity.Directory
}

type hive struct {
	// member namespace / subdomain where identities are created
	// "members" in alice.members.sf.club and "" in alice.sf.club
	memberDomain string

	// The domain at which identity's pear is hosted at (what goes in the #habitat) service in DID doc
	pearDomain string

	// The backing data store for hive
	store *store
}

var _ Hive = &hive{}

// toIdentity builds an identity.Identity from a stored IdentPublic and its known DID.
func idTemplateBuilder(memberDomain, pearDomain string) idTemplate {
	return func(handleInternal, opaqueID, signingPublicKey string) *identity.Identity {
		handle := syntax.Handle(handleInternal + "." + memberDomain)
		DID := syntax.DID("did:web:" + opaqueID + "." + memberDomain)
		return &identity.Identity{
			DID:         DID,
			Handle:      handle,
			AlsoKnownAs: []string{"at://" + string(handle)},
			Keys: map[string]identity.VerificationMethod{
				"atproto": {
					Type:               "Multikey",
					PublicKeyMultibase: signingPublicKey,
				},
			},
			Services: map[string]identity.ServiceEndpoint{
				"habitat": {
					Type: "HabitatServer",
					URL:  "https://" + pearDomain,
				},
			},
		}
	}
}

func NewHive(memberDomain string, pearDomain string, db *gorm.DB) (Hive, error) {
	template := idTemplateBuilder(memberDomain, pearDomain)
	store, err := newStore(db, template)
	if err != nil {
		return nil, err
	}
	h := &hive{
		memberDomain: memberDomain,
		pearDomain:   pearDomain,
		store:        store,
		// TODO: add a cache directory here
	}
	return h, nil
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
	// Validate DID
	// DID format: did:web:<opaqueID>.<baseDomain>
	content := strings.TrimPrefix(did.String(), "did:web:")
	opaqueID, after, ok := strings.Cut(content, ".")
	if after != h.memberDomain {
		return nil, identity.ErrDIDNotFound
	}
	if !ok {
		return nil, identity.ErrDIDNotFound
	}

	return h.store.getIdentityByID(ctx, opaqueID)
}

// LookupHandle implements identity.Directory
// It strips the internal handle prefix from the given handle which has format
// <internal-handle>.membersNamespace.domain or <internal-handle>.domain if membersNamespace == ""
// and looks up the handle against the store.
func (h *hive) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	// Validate handle
	internalHandle, after, ok := strings.Cut(handle.String(), ".")
	if after != h.memberDomain {
		return nil, identity.ErrHandleNotFound
	}
	if !ok {
		return nil, identity.ErrInvalidHandle
	}

	return h.store.getIdentityByHandle(ctx, internalHandle)
}

// Purge implements identity.Directory
func (h *hive) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	// Empty because we're not caching anything
	return nil
}

// MintIdentity implements Hive.
func (h *hive) MintIdentity(
	handlePrefix string,
	subdomain string,
) (*identity.Identity, func(*gorm.DB) error, error) {
	fullHandle := handlePrefix + "." + subdomain
	// Ensure handle passes regex
	if !handlePattern.MatchString(fullHandle) {
		return nil, nil, identity.ErrInvalidHandle
	}
	row, id, err := h.store.prepareIdentity(fullHandle)
	if err != nil {
		return nil, nil, err
	}
	return id, func(tx *gorm.DB) error {
		return persistIdentity(tx, row)
	}, nil
}
