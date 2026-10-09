// Package httpsig implements the small RFC 9421 (HTTP Message Signatures)
// profile used by atproto permissioned data (spaces) to bind a space
// credential to a key held by its syncer, replacing DPoP: a single signature
// labeled "atproto-space", made with a P-256 key (algorithm ecdsa-p256-sha256,
// RFC 9421 section 3.3.4) over a fixed list of request headers and identified
// by the signing key's did:key in the keyid parameter.
package httpsig

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

const (
	// Label names the signature in the Signature and Signature-Input headers.
	Label = "atproto-space"
	// AudienceHeader carries the DID of the party a credential is being
	// presented to; it is signed together with the credential.
	AudienceHeader = "Atproto-Space-Audience"
	// CredentialScheme is the Authorization scheme space credentials are
	// presented under.
	CredentialScheme = "Atproto-Space"

	// p256DIDKeyPrefix is the did:key prefix of every P-256 key (multicodec
	// 0x1200, multibase base58btc).
	p256DIDKeyPrefix = "did:key:zDn"
)

var (
	// ErrNoSignature means the request carries no atproto-space signature.
	ErrNoSignature = errors.New("no atproto-space signature")
	// ErrInvalidSignature means a signature is present but malformed or wrong.
	ErrInvalidSignature = errors.New("invalid http message signature")
)

// Signature-Input value for our label: atproto-space=("a" "b");keyid="did:key:...".
var sigInputRE = regexp.MustCompile(
	`^` + Label + `=\(((?:"[a-z0-9-]+"(?: "[a-z0-9-]+")*)?)\);keyid="([^"]+)"$`,
)

// HasSignature reports whether r carries an atproto-space signature, i.e.
// whether it opted in to signature-based key binding rather than legacy
// unbound credentials.
func HasSignature(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Signature-Input"), Label+"=")
}

// Sign signs r over the given lowercase header names with key and sets the
// Signature-Input and Signature headers. key must be a P-256 key.
func Sign(r *http.Request, key atcrypto.PrivateKey, components ...string) error {
	pub, err := key.PublicKey()
	if err != nil {
		return fmt.Errorf("get public key: %w", err)
	}
	didKey := pub.DIDKey()
	if !strings.HasPrefix(didKey, p256DIDKeyPrefix) {
		return errors.New("signing key must be P-256")
	}
	params := paramsValue(components, didKey)
	base, err := signatureBase(r, components, params)
	if err != nil {
		return err
	}
	// HashAndSign returns the 64-octet r||s encoding RFC 9421 requires.
	sig, err := key.HashAndSign([]byte(base))
	if err != nil {
		return fmt.Errorf("sign: %w", err)
	}
	r.Header.Set("Signature-Input", Label+"="+params)
	r.Header.Set("Signature", Label+"=:"+base64.StdEncoding.EncodeToString(sig)+":")
	return nil
}

// Verify checks r's atproto-space signature, which must cover exactly the
// given components in order, and returns the did:key that signed it. The
// caller decides whether that key is the one it expects (cnf.kid).
func Verify(r *http.Request, components ...string) (string, error) {
	input := r.Header.Get("Signature-Input")
	if !strings.HasPrefix(input, Label+"=") {
		return "", ErrNoSignature
	}
	m := sigInputRE.FindStringSubmatch(input)
	if m == nil {
		return "", fmt.Errorf("%w: malformed Signature-Input", ErrInvalidSignature)
	}
	covered := strings.ReplaceAll(m[1], `"`, "")
	if covered != strings.Join(components, " ") {
		return "", fmt.Errorf(
			"%w: signature must cover exactly %v",
			ErrInvalidSignature,
			components,
		)
	}
	didKey := m[2]
	if !strings.HasPrefix(didKey, p256DIDKeyPrefix) {
		return "", fmt.Errorf("%w: keyid must be a P-256 did:key", ErrInvalidSignature)
	}
	pub, err := atcrypto.ParsePublicDIDKey(didKey)
	if err != nil {
		return "", fmt.Errorf("%w: parse keyid: %w", ErrInvalidSignature, err)
	}
	sigHeader := r.Header.Get("Signature")
	encoded, ok := strings.CutPrefix(sigHeader, Label+"=:")
	if !ok || !strings.HasSuffix(encoded, ":") {
		return "", fmt.Errorf("%w: malformed Signature", ErrInvalidSignature)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSuffix(encoded, ":"))
	if err != nil {
		return "", fmt.Errorf("%w: decode signature: %w", ErrInvalidSignature, err)
	}
	base, err := signatureBase(r, components, paramsValue(components, didKey))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	// Lenient: RFC 9421 signers do not normalize to low-S.
	if err := pub.HashAndVerifyLenient([]byte(base), sig); err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	return didKey, nil
}

// paramsValue renders the signature parameters, e.g.
// ("authorization" "atproto-space-audience");keyid="did:key:...".
func paramsValue(components []string, didKey string) string {
	quoted := make([]string, len(components))
	for i, c := range components {
		quoted[i] = `"` + c + `"`
	}
	return "(" + strings.Join(quoted, " ") + `);keyid="` + didKey + `"`
}

// signatureBase builds the RFC 9421 section 2.5 signature base.
func signatureBase(r *http.Request, components []string, params string) (string, error) {
	var b strings.Builder
	for _, c := range components {
		v := strings.TrimSpace(r.Header.Get(c))
		if v == "" {
			return "", fmt.Errorf("covered header %q is missing", c)
		}
		fmt.Fprintf(&b, "\"%s\": %s\n", c, v)
	}
	fmt.Fprintf(&b, "\"@signature-params\": %s", params)
	return b.String(), nil
}
