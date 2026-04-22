package hive

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// hive roughly stands for "habitat identity verification and enrollment" and it is an identity
// service for habitat organizations.
// This package serves org identities based on the DID spec and the did:web: method to be legible to and interoperable
// with the broader AT protocol network, but allow orgs to own the user identities and management and
// not rely on the PLC directory as a central source of failure.

type Hive interface {
	// Minting new identities for members
	MintOrgMember(ctx context.Context, handle string)

	// Helpers for higher-level callers (like hive.Server)
	MemberDomain() string

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
	store store
}

func NewHive(domain string, memberNamespace string) (Hive, error) {
	return &hive{
		domain:          domain,
		memberNamespace: memberNamespace,
	}, nil
}

func (h *hive) MemberDomain() string {
	if h.memberNamespace == "" {
		return h.domain
	}
	return h.memberNamespace + "." + h.domain
}

// Lookup implements identity.Directory
func (h *hive) Lookup(ctx context.Context, atid syntax.AtIdentifier) (*identity.Identity, error) {
	panic("unimplemented")
}

// LookupDID implements identity.Directory
func (h *hive) LookupDID(ctx context.Context, did syntax.DID) (*identity.Identity, error) {
	panic("unimplemented")
}

// LookupHandle implements identity.Directory
// It strips the internal handle prefix from the given handle which has format
// <internal-handle>.membersNamespace.domain or <internal-handle>.domain if membersNamespace == ""
// and looks up the handle against the store.
func (h *hive) LookupHandle(ctx context.Context, handle syntax.Handle) (*identity.Identity, error) {
	panic("unimplemented")
}

// Purge implements identity.Directory
func (h *hive) Purge(ctx context.Context, atid syntax.AtIdentifier) error {
	panic("unimplemented")
}

// MintOrgMember implements Hive.
func (h *hive) MintOrgMember(ctx context.Context, handle string) {
	panic("unimplemented")
}

var _ Hive = &hive{}
