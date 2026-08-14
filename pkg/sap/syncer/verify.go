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
	pub, err := v.signer(ctx, space, author)
	if err != nil {
		return fmt.Errorf("resolve signer for %s: %w", author, err)
	}
	return spacecommit.Verify(c, space, author, lt.Sum(), pub)
}

// signer resolves the public key that authenticated the commit, mirroring the
// host's signing choice in spacecommit.Authority.
func (v *Verifier) signer(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
) (atcrypto.PublicKey, error) {
	// Habitat-managed identities are did:web accounts whose signing keys the
	// hive holds; the host signs their commits with their own key.
	if author.Method() == "web" {
		ident, err := v.dir.LookupDID(ctx, author)
		if err != nil {
			return nil, fmt.Errorf("lookup author: %w", err)
		}
		pub, err := ident.PublicKey()
		if err != nil {
			return nil, fmt.Errorf("author signing key: %w", err)
		}
		return pub, nil
	}

	// External authors: the space host signed with its own key. Per the
	// proposal's space-authority resolution, discover the host through the
	// space owner's DID doc "#atproto_space_host" service, then read the
	// signing key from the host DID doc's "#atproto_space" verification
	// method, falling back to "#atproto" when the host publishes no dedicated
	// space key.
	owner := space.SpaceOwner()
	ownerIdent, err := v.dir.LookupDID(ctx, owner)
	if err != nil {
		return nil, fmt.Errorf("lookup space owner: %w", err)
	}
	svc, ok := ownerIdent.Services["atproto_space_host"]
	if !ok || svc.URL == "" {
		return nil, fmt.Errorf("space owner %s has no atproto_space_host service", owner)
	}
	u, err := url.Parse(svc.URL)
	if err != nil {
		return nil, fmt.Errorf("parse atproto_space_host service url: %w", err)
	}
	// did:web encodes a port's colon as %3A.
	hostDID := syntax.DID("did:web:" + strings.ReplaceAll(u.Host, ":", "%3A"))
	hostIdent, err := v.dir.LookupDID(ctx, hostDID)
	if err != nil {
		return nil, fmt.Errorf("lookup host %s: %w", hostDID, err)
	}
	pub, err := hostIdent.GetPublicKey("atproto_space")
	if err != nil {
		pub, err = hostIdent.GetPublicKey("atproto")
	}
	if err != nil {
		return nil, fmt.Errorf("host signing key: %w", err)
	}
	return pub, nil
}

// decodeCommit maps the lexicon JSON form of a signed commit (bytes fields are
// {"$bytes": "<base64>"} objects per the atproto lexicon spec) to its
// in-memory form.
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
// map[string]any of the form {"$bytes": "<base64>"}, into raw bytes. Absent
// fields decode to nil.
func decodeBytesField(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	values, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("bytes field is not a map[string]any")
	}
	s, ok := values["$bytes"].(string)
	if !ok {
		return nil, fmt.Errorf("bytes field $bytes is not a string")
	}
	b, err := base64.RawStdEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("decode bytes field: %w", err)
	}
	return []byte(b), nil
}
