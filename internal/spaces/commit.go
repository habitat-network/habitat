package spaces

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
	// commitVersion is the signed-commit format version.
	commitVersion = 1
	// specProtocolTag prefixes the ctx for habitat-managed authors, whose commits
	// are signed by the repo owner's own signing key per the atproto
	// permissioned-data proposal.
	specProtocolTag = "atproto-space-v1"
	// hostProtocolTag prefixes the ctx for authors whose identity lives on a PDS
	// we do not control. Habitat cannot use their signing key, so it signs with
	// its single host key instead; the distinct tag keeps these signatures from
	// being confused with proposal-spec commits.
	hostProtocolTag = "habitat-space-host-v1"
	// commitIKMLen is the per-commit nonce length in bytes.
	commitIKMLen = 32
)

var (
	errNoCommitSigner = errors.New("no commit signer available for author")
	// errEmptyRepo signals that a repo holds no records, so there is nothing to
	// commit over. It is handled internally and never returned to clients.
	errEmptyRepo = errors.New("repo has no records")
)

// MemberSigner signs on behalf of habitat-managed identities (did:web hive
// members whose keys Habitat holds). For those authors we follow the proposal
// and sign with the repo owner's own key.
type MemberSigner interface {
	PrivateKeyForDID(ctx context.Context, did syntax.DID) (atcrypto.PrivateKey, error)
}

// commitAuthority selects how to sign a commit for a given author: the repo
// owner's own key (habitat-managed authors, spec tag) or the host key
// (non-managed authors, host tag).
type commitAuthority struct {
	host   atcrypto.PrivateKey
	member MemberSigner
}

// resolve returns the protocol tag and signing function for author.
func (a *commitAuthority) resolve(
	ctx context.Context,
	author syntax.DID,
) (tag string, privKey atcrypto.PrivateKey, err error) {
	privKey, err = a.member.PrivateKeyForDID(ctx, author)
	if errors.Is(err, identity.ErrDIDNotFound) {
		return hostProtocolTag, a.host, nil
	} else if err != nil {
		return "", nil, err
	}
	return specProtocolTag, privKey, nil
}

// signedCommit is the in-memory form of network.habitat.space.defs#signedCommit.
type signedCommit struct {
	Ver  int
	Hash []byte
	Ikm  []byte
	Mac  []byte
	Sig  []byte
	Rev  string
}

// commitCtx builds the ctx byte string: the protocol tag followed by each
// variable field length-prefixed with a big-endian uint16 (TLS 1.3 vector
// convention): space, author DID, rev, ikm.
func commitCtx(
	tag string,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	rev string,
	ikm []byte,
) []byte {
	b := make([]byte, 0, len(tag)+len(space)+len(author)+len(rev)+len(ikm)+8)
	b = append(b, []byte(tag)...)
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

// buildSignedCommit assembles a signed commit over a repo's current hash for a
// single reader. A fresh ikm is generated per call (per the proposal, a new ikm
// per reader), so mac/sig differ between calls even for the same hash.
func buildSignedCommit(
	ctx context.Context,
	authority *commitAuthority,
	space habitat_syntax.SpaceURI,
	author syntax.DID,
	rev string,
	hash []byte,
) (signedCommit, error) {
	tag, privKey, err := authority.resolve(ctx, author)
	if err != nil {
		return signedCommit{}, err
	}

	ikm := make([]byte, commitIKMLen)
	if _, err := io.ReadFull(rand.Reader, ikm); err != nil {
		return signedCommit{}, fmt.Errorf("generate ikm: %w", err)
	}
	cctx := commitCtx(tag, space, author, rev, ikm)

	// mac = HMAC-SHA256(HKDF-SHA256(ikm, info=ctx), hash): binds the repo hash to
	// this commit's context without the signature covering the hash.
	macKey := make([]byte, sha256.Size)
	if _, err := io.ReadFull(hkdf.New(sha256.New, ikm, nil, cctx), macKey); err != nil {
		return signedCommit{}, fmt.Errorf("derive mac key: %w", err)
	}
	m := hmac.New(sha256.New, macKey)
	_, _ = m.Write(hash)
	mac := m.Sum(nil)

	sig, err := privKey.HashAndSign(cctx)
	if err != nil {
		return signedCommit{}, fmt.Errorf("sign commit ctx: %w", err)
	}

	return signedCommit{
		Ver:  commitVersion,
		Hash: hash,
		Ikm:  ikm,
		Mac:  mac,
		Sig:  sig,
		Rev:  rev,
	}, nil
}
