package syncer

import (
	"context"
	"crypto/hmac"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Verifier authenticates a repo's signed commit against a locally recomputed
// LtHash, resolving signer keys by the author's identity type: habitat-managed
// (did:web) authors sign their own commits per the proposal spec, while
// external authors' commits are signed by the space host's key, published in
// the host's DID document under the "habitat" verification method.
type Verifier struct {
	dir identity.Directory
}

// NewVerifier builds a Verifier. A nil directory (or nil Verifier) degrades to
// hash-only verification: the commit's hash is compared but its signature is
// not checked.
func NewVerifier(dir identity.Directory) *Verifier {
	return &Verifier{dir: dir}
}

// Verify checks c against the folded LtHash for (space, author). It returns an
// error wrapping spacecommit.ErrInvalidCommit when the commit fails
// validation, and other errors when the signer key cannot be resolved
// (transient: identity lookups may fail).
func (v *Verifier) Verify(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	c spacecommit.SignedCommit,
	lt *spacecommit.LtHash,
) error {
	if v == nil || v.dir == nil {
		if !hmac.Equal(lt.Sum(), c.Hash) {
			return fmt.Errorf("%w: hash mismatch", spacecommit.ErrInvalidCommit)
		}
		return nil
	}
	tag, pub, err := v.signer(ctx, space, author)
	if err != nil {
		return fmt.Errorf("resolve signer for %s: %w", author, err)
	}
	return spacecommit.Verify(c, tag, space, author, lt.Sum(), pub)
}

// signer resolves the protocol tag and public key that authenticated the
// commit, mirroring the host's signing choice in spacecommit.Authority.
func (v *Verifier) signer(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
) (string, atcrypto.PublicKey, error) {
	// Habitat-managed identities are did:web accounts whose signing keys the
	// hive holds; the host signs their commits with their own key (spec tag).
	if author.Method() == "web" {
		ident, err := v.dir.LookupDID(ctx, author)
		if err != nil {
			return "", nil, fmt.Errorf("lookup author: %w", err)
		}
		pub, err := ident.PublicKey()
		if err != nil {
			return "", nil, fmt.Errorf("author signing key: %w", err)
		}
		return spacecommit.SpecProtocolTag, pub, nil
	}

	// External authors: the host signed with its own key. Discover the host
	// through the space owner's DID doc (its "habitat" service endpoint), then
	// read the key from the host DID doc's "habitat" verification method.
	owner := space.SpaceOwner()
	ownerIdent, err := v.dir.LookupDID(ctx, owner)
	if err != nil {
		return "", nil, fmt.Errorf("lookup space owner: %w", err)
	}
	svc, ok := ownerIdent.Services["habitat"]
	if !ok || svc.URL == "" {
		return "", nil, fmt.Errorf("space owner %s has no habitat service", owner)
	}
	u, err := url.Parse(svc.URL)
	if err != nil {
		return "", nil, fmt.Errorf("parse habitat service url: %w", err)
	}
	// did:web encodes a port's colon as %3A.
	hostDID := syntax.DID("did:web:" + strings.ReplaceAll(u.Host, ":", "%3A"))
	hostIdent, err := v.dir.LookupDID(ctx, hostDID)
	if err != nil {
		return "", nil, fmt.Errorf("lookup host %s: %w", hostDID, err)
	}
	pub, err := hostIdent.GetPublicKey("habitat")
	if err != nil {
		return "", nil, fmt.Errorf("host signing key: %w", err)
	}
	return spacecommit.HostProtocolTag, pub, nil
}

// decodeCommit maps the lexicon JSON form of a signed commit (bytes fields are
// base64 strings) to its in-memory form.
func decodeCommit(c habitat.NetworkHabitatSpaceDefsSignedCommit) (spacecommit.SignedCommit, error) {
	hash, err := decodeBytesField(c.Hash)
	if err != nil {
		return spacecommit.SignedCommit{}, fmt.Errorf("decode commit hash: %w", err)
	}
	ikm, err := decodeBytesField(c.Ikm)
	if err != nil {
		return spacecommit.SignedCommit{}, fmt.Errorf("decode commit ikm: %w", err)
	}
	mac, err := decodeBytesField(c.Mac)
	if err != nil {
		return spacecommit.SignedCommit{}, fmt.Errorf("decode commit mac: %w", err)
	}
	sig, err := decodeBytesField(c.Sig)
	if err != nil {
		return spacecommit.SignedCommit{}, fmt.Errorf("decode commit sig: %w", err)
	}
	return spacecommit.SignedCommit{
		Ver:  int(c.Ver),
		Hash: hash,
		Ikm:  ikm,
		Mac:  mac,
		Sig:  sig,
		Rev:  c.Rev,
	}, nil
}

// decodeBytesField decodes a lexicon bytes field, which JSON-decodes into a
// base64 (std) string, into raw bytes. Absent fields decode to nil.
func decodeBytesField(v any) ([]byte, error) {
	switch s := v.(type) {
	case nil:
		return nil, nil
	case string:
		return base64.StdEncoding.DecodeString(s)
	default:
		return nil, fmt.Errorf("expected base64 string, got %T", v)
	}
}
