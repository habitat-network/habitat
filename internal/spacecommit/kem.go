package spacecommit

import (
	"context"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// The key encapsulation mechanism (KEM) establishes the shared secret a syncer
// and repo host MAC a response with. It is ECDH on P-256, with the secret
// derived as in DHKEM(P-256, HKDF-SHA256) from RFC 9180 §4.1, the syncer's
// ephemeral public key being `enc`:
//
//	dh     = ECDH(syncer_private_key, author_public_key)   // syncer
//	       = ECDH(author_private_key, syncer_public_key)   // repo host
//	keys   = (0x04 || syncer_pub.x || syncer_pub.y) || (0x04 || author_pub.x || author_pub.y)
//	prefix = "HPKE-v1" || "KEM" || uint16be(0x0010)
//	input  = prefix || "eae_prk" || dh
//	info   = uint16be(32) || prefix || "shared_secret" || keys
//	shared_secret = HKDF-SHA256(input, salt="", info=info, length=32)
//
// Each account's KEM public key is published in its DID document as the
// #atproto_kem verification method, and its private key is held by the repo
// host. It is a dedicated keypair, not the #atproto signing key.

// KEMFragment is the DID document verification method holding an account's
// KEM public key, without the leading '#'.
const KEMFragment = "atproto_kem"

const sharedSecretLen = 32

// kemPrefix is RFC 9180's suite ID for DHKEM(P-256, HKDF-SHA256).
var kemPrefix = []byte("HPKE-v1KEM\x00\x10")

// Encapsulate is the syncer side: it generates a fresh ephemeral P-256
// keypair, derives a shared secret against the author's KEM key, and returns
// the ephemeral public key as a did:key to send as the request's key_share.
// Call it for every request, including retries and each page of a paginated
// response.
func Encapsulate(authorKEM atcrypto.PublicKey) (keyShare string, sharedSecret []byte, err error) {
	authorPub, err := ecdhPublic(authorKEM)
	if err != nil {
		return "", nil, err
	}
	ephemeral, err := atcrypto.GeneratePrivateKeyP256()
	if err != nil {
		return "", nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	ephemeralPriv, err := ecdh.P256().NewPrivateKey(ephemeral.Bytes())
	if err != nil {
		return "", nil, fmt.Errorf("ephemeral key: %w", err)
	}
	dh, err := ephemeralPriv.ECDH(authorPub)
	if err != nil {
		return "", nil, fmt.Errorf("%w: ecdh: %w", ErrBadSpaceMAC, err)
	}
	ephemeralPub, err := ephemeral.PublicKey()
	if err != nil {
		return "", nil, err
	}
	sharedSecret, err = deriveSharedSecret(
		dh, ephemeralPriv.PublicKey().Bytes(), authorPub.Bytes(),
	)
	if err != nil {
		return "", nil, err
	}
	return ephemeralPub.DIDKey(), sharedSecret, nil
}

// Decapsulate is the repo host side: it derives the shared secret from a
// syncer's key_share using the author's KEM private key. An invalid key share
// wraps [ErrBadSpaceMAC].
func Decapsulate(kem *atcrypto.PrivateKeyP256, keyShare string) ([]byte, error) {
	syncerKey, err := atcrypto.ParsePublicDIDKey(keyShare)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid key_share: %w", ErrBadSpaceMAC, err)
	}
	syncerPub, err := ecdhPublic(syncerKey)
	if err != nil {
		return nil, err
	}
	priv, err := ecdh.P256().NewPrivateKey(kem.Bytes())
	if err != nil {
		return nil, fmt.Errorf("kem key: %w", err)
	}
	dh, err := priv.ECDH(syncerPub)
	if err != nil {
		return nil, fmt.Errorf("%w: ecdh: %w", ErrBadSpaceMAC, err)
	}
	return deriveSharedSecret(dh, syncerPub.Bytes(), priv.PublicKey().Bytes())
}

// ecdhPublic converts a P-256 public key to its crypto/ecdh form. Any other
// curve wraps [ErrBadSpaceMAC]: KEM keys are always P-256.
func ecdhPublic(k atcrypto.PublicKey) (*ecdh.PublicKey, error) {
	p256, ok := k.(*atcrypto.PublicKeyP256)
	if !ok {
		return nil, fmt.Errorf("%w: KEM key must be a P-256 key", ErrBadSpaceMAC)
	}
	pub, err := ecdh.P256().NewPublicKey(p256.UncompressedBytes())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid KEM key: %w", ErrBadSpaceMAC, err)
	}
	return pub, nil
}

// deriveSharedSecret is RFC 9180's ExtractAndExpand over the ECDH output, with
// both public keys in their uncompressed form, syncer's first.
func deriveSharedSecret(dh, syncerPub, authorPub []byte) ([]byte, error) {
	input := make([]byte, 0, len(kemPrefix)+len("eae_prk")+len(dh))
	input = append(input, kemPrefix...)
	input = append(input, "eae_prk"...)
	input = append(input, dh...)

	info := binary.BigEndian.AppendUint16(nil, sharedSecretLen)
	info = append(info, kemPrefix...)
	info = append(info, "shared_secret"...)
	info = append(info, syncerPub...)
	info = append(info, authorPub...)

	secret, err := hkdf.Key(sha256.New, input, nil, string(info), sharedSecretLen)
	if err != nil {
		return nil, fmt.Errorf("derive shared secret: %w", err)
	}
	return secret, nil
}

// DeriveKEMKey deterministically derives a dedicated P-256 KEM keypair from
// another private key, so a host can publish a KEM key alongside its signing
// key without provisioning a second secret. The derived key is unrelated to
// seed as far as anyone without seed can tell: it is HKDF output under a
// fixed label, never the seed key itself.
func DeriveKEMKey(seed atcrypto.PrivateKeyExportable) (*atcrypto.PrivateKeyP256, error) {
	// A uniformly random 32-byte string is a valid P-256 scalar with
	// overwhelming probability; the counter only guards the remainder.
	for i := range 16 {
		b, err := hkdf.Key(
			sha256.New, seed.Bytes(), nil, fmt.Sprintf("habitat-kem-key-v1/%d", i), 32,
		)
		if err != nil {
			return nil, fmt.Errorf("derive kem key: %w", err)
		}
		if key, err := atcrypto.ParsePrivateBytesP256(b); err == nil {
			return key, nil
		}
	}
	return nil, errors.New("derive kem key: no valid scalar")
}

// MemberKEMKeys resolves the KEM private key of a habitat-managed identity
// (a did:web hive member whose keys Habitat holds). It returns
// identity.ErrDIDNotFound for DIDs it does not manage.
type MemberKEMKeys interface {
	KEMKeyForDID(ctx context.Context, did syntax.DID) (*atcrypto.PrivateKeyP256, error)
}

// KEMKeys selects which KEM key a repo host decapsulates with for an author:
// the author's own key, published as their #atproto_kem, for habitat-managed
// authors, or the host's KEM key, published as the host's own #atproto_kem,
// for authors whose identity lives on a PDS we do not control (and so whose
// DID document Habitat cannot publish a key in). A syncer makes the same
// choice from the author's DID document before the request; see
// sap's syncer.Verifier.
type KEMKeys struct {
	host   *atcrypto.PrivateKeyP256
	member MemberKEMKeys
}

// NewKEMKeys builds a KEMKeys. host is Habitat's host KEM key (may be nil, in
// which case only habitat-managed authors can be served MACs).
func NewKEMKeys(host *atcrypto.PrivateKeyP256, member MemberKEMKeys) *KEMKeys {
	return &KEMKeys{host: host, member: member}
}

// Decapsulate derives the shared secret for a request about author's repo
// from the syncer's key share.
func (k *KEMKeys) Decapsulate(
	ctx context.Context,
	author syntax.DID,
	keyShare string,
) ([]byte, error) {
	key, err := k.member.KEMKeyForDID(ctx, author)
	if errors.Is(err, identity.ErrDIDNotFound) {
		key, err = k.host, nil
	}
	if err != nil {
		return nil, fmt.Errorf("kem key for %s: %w", author, err)
	}
	if key == nil {
		return nil, fmt.Errorf("%w: no kem key for %s", ErrBadSpaceMAC, author)
	}
	return Decapsulate(key, keyShare)
}
