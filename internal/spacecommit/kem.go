package spacecommit

import (
	"context"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/hpke"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
)

// The key encapsulation mechanism (KEM) establishes the shared secret a syncer
// and repo host MAC a response with. It is the secret export feature of HPKE
// (RFC 9180 section 6.2) with the ciphersuite DHKEM(P-256, HKDF-SHA256),
// HKDF-SHA256 and the Export-only AEAD, so nothing is encrypted:
//
//	enc, ctx = SetupBaseS(author_kem_public_key, info="atproto-mac")   // syncer
//	ctx      = SetupBaseR(enc, author_kem_private_key, info="atproto-mac") // repo host
//	shared_secret = ctx.Export(exporter_context="", L=32)
//
// The syncer's ephemeral public key `enc` is sent as the keyid of its
// Accept-Signature request, as a did:key.
//
// Each account's KEM public key is published in its DID document as the
// #atproto_kem verification method, and its private key is held by the repo
// host. It is a dedicated keypair, not the #atproto signing key.

// KEMFragment is the DID document verification method holding an account's
// KEM public key, without the leading '#'.
const KEMFragment = "atproto_kem"

const sharedSecretLen = 32

// hpkeInfo is a fixed label tying the secret to this protocol.
var hpkeInfo = []byte("atproto-mac")

// Encapsulate is the syncer side: it generates a fresh ephemeral P-256
// keypair, derives a shared secret against the author's KEM key, and returns
// the ephemeral public key as a did:key to send as the request's keyid. Call it
// for every request, including retries and each page of a paginated response.
func Encapsulate(authorKEM atcrypto.PublicKey) (keyShare string, sharedSecret []byte, err error) {
	authorPub, err := ecdhPublic(authorKEM)
	if err != nil {
		return "", nil, err
	}
	pk, err := hpke.NewDHKEMPublicKey(authorPub)
	if err != nil {
		return "", nil, fmt.Errorf("%w: invalid KEM key: %w", ErrInvalidSpaceMAC, err)
	}
	enc, sender, err := hpke.NewSender(pk, hpke.HKDFSHA256(), hpke.ExportOnly(), hpkeInfo)
	if err != nil {
		return "", nil, fmt.Errorf("hpke setup: %w", err)
	}
	// HPKE encodes the encapsulated key uncompressed; did:key uses the
	// compressed form.
	ephemeral, err := atcrypto.ParsePublicUncompressedBytesP256(enc)
	if err != nil {
		return "", nil, fmt.Errorf("encapsulated key: %w", err)
	}
	sharedSecret, err = sender.Export("", sharedSecretLen)
	if err != nil {
		return "", nil, fmt.Errorf("hpke export: %w", err)
	}
	return ephemeral.DIDKey(), sharedSecret, nil
}

// Decapsulate is the repo host side: it derives the shared secret from a
// syncer's keyid using the author's KEM private key. An invalid key share
// wraps [ErrInvalidSpaceMAC].
func Decapsulate(kem *atcrypto.PrivateKeyP256, keyShare string) ([]byte, error) {
	syncerKey, err := atcrypto.ParsePublicDIDKey(keyShare)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid keyid: %w", ErrInvalidSpaceMAC, err)
	}
	syncerPub, err := ecdhPublic(syncerKey)
	if err != nil {
		return nil, err
	}
	priv, err := ecdh.P256().NewPrivateKey(kem.Bytes())
	if err != nil {
		return nil, fmt.Errorf("kem key: %w", err)
	}
	sk, err := hpke.NewDHKEMPrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("kem key: %w", err)
	}
	recipient, err := hpke.NewRecipient(
		syncerPub.Bytes(), sk, hpke.HKDFSHA256(), hpke.ExportOnly(), hpkeInfo,
	)
	if err != nil {
		return nil, fmt.Errorf("%w: hpke setup: %w", ErrInvalidSpaceMAC, err)
	}
	secret, err := recipient.Export("", sharedSecretLen)
	if err != nil {
		return nil, fmt.Errorf("hpke export: %w", err)
	}
	return secret, nil
}

// ecdhPublic converts a P-256 public key to its crypto/ecdh form. Any other
// curve wraps [ErrInvalidSpaceMAC]: KEM keys are always P-256.
func ecdhPublic(k atcrypto.PublicKey) (*ecdh.PublicKey, error) {
	p256, ok := k.(*atcrypto.PublicKeyP256)
	if !ok {
		return nil, fmt.Errorf("%w: KEM key must be a P-256 key", ErrInvalidSpaceMAC)
	}
	pub, err := ecdh.P256().NewPublicKey(p256.UncompressedBytes())
	if err != nil {
		return nil, fmt.Errorf("%w: invalid KEM key: %w", ErrInvalidSpaceMAC, err)
	}
	return pub, nil
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
		return nil, fmt.Errorf("%w: no kem key for %s", ErrInvalidSpaceMAC, author)
	}
	return Decapsulate(key, keyShare)
}
