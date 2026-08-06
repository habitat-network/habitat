package spacecommit

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"golang.org/x/crypto/hkdf"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	// Version is the signed-commit format version.
	Version = 1
	// ProtocolTag is the domain-separation prefix of the commit ctx, fixed by the
	// atproto permissioned-data proposal.
	ProtocolTag = "atproto-space-v1"
	// ikmLen is the per-commit nonce length in bytes.
	ikmLen = 32
)

// ErrInvalidCommit is returned by Verify when a commit fails validation.
var ErrInvalidCommit = errors.New("invalid commit")

// MemberSigner resolves the signing key for habitat-managed identities (did:web
// hive members whose keys Habitat holds). It returns identity.ErrDIDNotFound for
// DIDs it does not manage.
type MemberSigner interface {
	PrivateKeyForDID(ctx context.Context, did syntax.DID) (atcrypto.PrivateKey, error)
}

// Authority selects which key signs a commit for a given author: the repo
// owner's own key for habitat-managed authors, or the host key for authors
// whose identity lives on a PDS we do not control. The commit itself carries no
// indication of which was used, so verifiers must know out of band which key to
// check a given author's commits against.
type Authority struct {
	host   atcrypto.PrivateKey
	member MemberSigner
}

// NewAuthority builds a commit Authority. host is Habitat's single host signing
// key (may be nil, in which case only habitat-managed authors can be signed).
// member resolves habitat-managed owners' own keys.
func NewAuthority(host atcrypto.PrivateKey, member MemberSigner) *Authority {
	return &Authority{host: host, member: member}
}

// resolve returns the signing key for author.
func (a *Authority) resolve(
	ctx context.Context,
	author syntax.DID,
) (atcrypto.PrivateKey, error) {
	key, err := a.member.PrivateKeyForDID(ctx, author)
	if err == nil {
		return key, nil
	}
	if !errors.Is(err, identity.ErrDIDNotFound) {
		return nil, err
	}
	return a.host, nil
}

// SignedCommit is the in-memory form of network.habitat.space.defs#signedCommit.
type SignedCommit struct {
	Ver  int
	Hash []byte
	Ikm  []byte
	Mac  []byte
	Sig  []byte
	Rev  string
}

// Ctx builds the commit ctx byte string: the protocol tag followed by each
// variable field length-prefixed with a big-endian uint16 (TLS 1.3 vector
// convention): space, author DID, rev, ikm.
func Ctx(
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	rev string,
	ikm []byte,
) []byte {
	b := make([]byte, 0, len(ProtocolTag)+len(space)+len(author)+len(rev)+len(ikm)+8)
	b = append(b, []byte(ProtocolTag)...)
	b = appendVec(b, []byte(space.String()))
	b = appendVec(b, []byte(author.String()))
	b = appendVec(b, []byte(rev))
	b = appendVec(b, ikm)
	return b
}

// appendVec appends field to b length-prefixed with a big-endian uint16.
func appendVec(b, field []byte) []byte {
	var l [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(field)))
	b = append(b, l[:]...)
	return append(b, field...)
}

// mac computes HMAC-SHA256(HKDF-SHA256(ikm, info=ctx), hash), binding the repo
// hash to the commit's context without the signature covering the hash.
func mac(ikm, ctx, hash []byte) ([]byte, error) {
	key := make([]byte, sha256.Size)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, nil, ctx), key); err != nil {
		return nil, fmt.Errorf("derive mac key: %w", err)
	}
	m := hmac.New(sha256.New, key)
	_, _ = m.Write(hash)
	return m.Sum(nil), nil
}

// Build assembles a signed commit over a repo's current hash for a single
// reader. A fresh ikm is generated per call (per the proposal, a new ikm per
// reader), so mac/sig differ between calls even for the same hash.
func (a *Authority) Build(
	ctx context.Context,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	rev string,
	hash []byte,
) (SignedCommit, error) {
	privKey, err := a.resolve(ctx, author)
	if err != nil {
		return SignedCommit{}, err
	}

	ikm := make([]byte, ikmLen)
	if _, err := io.ReadFull(rand.Reader, ikm); err != nil {
		return SignedCommit{}, fmt.Errorf("generate ikm: %w", err)
	}
	cctx := Ctx(space, author, rev, ikm)

	macBytes, err := mac(ikm, cctx, hash)
	if err != nil {
		return SignedCommit{}, err
	}

	sig, err := privKey.HashAndSign(cctx)
	if err != nil {
		return SignedCommit{}, fmt.Errorf("sign commit ctx: %w", err)
	}

	return SignedCommit{
		Ver:  Version,
		Hash: hash,
		Ikm:  ikm,
		Mac:  macBytes,
		Sig:  sig,
		Rev:  rev,
	}, nil
}

// Verify checks a commit against a locally recomputed hash and pub. It confirms
// the commit's hash matches expectedHash, the mac binds that hash to the ctx,
// and the signature verifies over the ctx. Returns ErrInvalidCommit on any
// mismatch.
//
// Commits carry no marker for which key signed them, so the caller must supply
// the right pub: the author's own key for habitat-managed authors, the host key
// for everyone else.
func Verify(
	c SignedCommit,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	expectedHash []byte,
	pub atcrypto.PublicKey,
) error {
	if !hmac.Equal(c.Hash, expectedHash) {
		return fmt.Errorf("%w: hash mismatch", ErrInvalidCommit)
	}
	cctx := Ctx(space, author, c.Rev, c.Ikm)
	wantMac, err := mac(c.Ikm, cctx, c.Hash)
	if err != nil {
		return err
	}
	if !hmac.Equal(c.Mac, wantMac) {
		return fmt.Errorf("%w: mac mismatch", ErrInvalidCommit)
	}
	if err := pub.HashAndVerify(cctx, c.Sig); err != nil {
		return fmt.Errorf("%w: bad signature: %w", ErrInvalidCommit, err)
	}
	return nil
}
