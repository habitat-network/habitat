// Package httpsig implements the small RFC 9421 (HTTP Message Signatures)
// profile used by atproto permissioned data (spaces) to bind a space
// credential to a key held by its syncer, replacing DPoP: a single signature
// labeled "atproto-space-credential", tagged "atproto-space-credential", made
// with a P-256 key (algorithm ecdsa-p256-sha256, RFC 9421 section 3.3.4) over
// a fixed list of request headers. The signing key is the credential's
// cnf.kid, so the signature need not name it.
package httpsig

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
)

const (
	// Label names the signature in the Signature and Signature-Input headers.
	Label = "atproto-space-credential"
	// Tag is the required value of the signature's tag parameter.
	Tag = "atproto-space-credential"
	// Alg is the only algorithm a signature may name in its optional alg
	// parameter.
	Alg = "ecdsa-p256-sha256"
	// AudienceHeader carries the DID of the party a credential is being
	// presented to; it is signed together with the credential.
	AudienceHeader = "Atproto-Space-Audience"
	// CredentialScheme is the Authorization scheme space credentials are
	// presented under. Receivers match it case-insensitively.
	CredentialScheme = "Atproto-Space-Credential"

	// p256DIDKeyPrefix is the did:key prefix of every P-256 key (multicodec
	// 0x1200, multibase base58btc).
	p256DIDKeyPrefix = "did:key:zDn"
)

var (
	// ErrNoSignature means the request carries no atproto-space-credential
	// signature.
	ErrNoSignature = errors.New("no atproto-space-credential signature")
	// ErrInvalidSignature means a signature is present but malformed or wrong.
	ErrInvalidSignature = errors.New("invalid http message signature")
)

// IsP256DIDKey reports whether didKey is a P-256 did:key, the only kind of
// key a space credential can be bound to.
func IsP256DIDKey(didKey string) bool {
	if !strings.HasPrefix(didKey, p256DIDKeyPrefix) {
		return false
	}
	_, err := atcrypto.ParsePublicDIDKey(didKey)
	return err == nil
}

// Sign signs r over the given lowercase header names with key and sets the
// Signature-Input and Signature headers. key must be a P-256 key.
func Sign(r *http.Request, key atcrypto.PrivateKey, components ...string) error {
	pub, err := key.PublicKey()
	if err != nil {
		return fmt.Errorf("get public key: %w", err)
	}
	if !strings.HasPrefix(pub.DIDKey(), p256DIDKeyPrefix) {
		return errors.New("signing key must be P-256")
	}
	params := componentList(components) + `;tag="` + Tag + `"`
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
	// Padded standard base64, per RFC 9651 byte sequences.
	r.Header.Set("Signature", Label+"=:"+base64.StdEncoding.EncodeToString(sig)+":")
	return nil
}

// VerifyBound checks r's atproto-space-credential signature, which must cover
// exactly the given components in order and carry the atproto-space-credential
// tag, against boundKey: the P-256 did:key a space credential is bound to (its
// cnf.kid). The signature's keyid parameter may be omitted, as @atproto/space's
// client does, but must match boundKey if present. Other signatures in the
// same headers, under other labels, are ignored.
func VerifyBound(r *http.Request, boundKey string, components ...string) error {
	input, ok := dictMember(r.Header.Values("Signature-Input"), Label)
	if !ok {
		return ErrNoSignature
	}
	covered, params, err := parseInput(input)
	if err != nil {
		return err
	}
	if covered != componentList(components) {
		return fmt.Errorf(
			"%w: signature must cover exactly %v",
			ErrInvalidSignature,
			components,
		)
	}
	if params["tag"] != Tag {
		return fmt.Errorf("%w: signature tag must be %q", ErrInvalidSignature, Tag)
	}
	if alg, ok := params["alg"]; ok && alg != Alg {
		return fmt.Errorf("%w: signature algorithm must be %s", ErrInvalidSignature, Alg)
	}
	if keyID, ok := params["keyid"]; ok && keyID != boundKey {
		return fmt.Errorf("%w: keyid does not match the bound key", ErrInvalidSignature)
	}
	if !strings.HasPrefix(boundKey, p256DIDKeyPrefix) {
		return fmt.Errorf("%w: bound key must be a P-256 did:key", ErrInvalidSignature)
	}
	pub, err := atcrypto.ParsePublicDIDKey(boundKey)
	if err != nil {
		return fmt.Errorf("%w: parse bound key: %w", ErrInvalidSignature, err)
	}
	sigItem, ok := dictMember(r.Header.Values("Signature"), Label)
	if !ok {
		return fmt.Errorf("%w: missing Signature", ErrInvalidSignature)
	}
	encoded, ok := strings.CutPrefix(sigItem, ":")
	if !ok || !strings.HasSuffix(encoded, ":") {
		return fmt.Errorf("%w: malformed Signature", ErrInvalidSignature)
	}
	// Accept padded (RFC 9651) and unpadded base64, as the reference does.
	sig, err := base64.RawStdEncoding.DecodeString(
		strings.TrimRight(strings.TrimSuffix(encoded, ":"), "="),
	)
	if err != nil {
		return fmt.Errorf("%w: decode signature: %w", ErrInvalidSignature, err)
	}
	if len(sig) != 64 {
		return fmt.Errorf("%w: signature must be 64 bytes", ErrInvalidSignature)
	}
	// The base covers the parameters exactly as sent in Signature-Input.
	base, err := signatureBase(r, components, input)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	// Lenient: RFC 9421 signers do not normalize to low-S.
	if err := pub.HashAndVerifyLenient([]byte(base), sig); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidSignature, err)
	}
	return nil
}

// componentList renders the covered components as an inner list, e.g.
// ("authorization" "atproto-space-audience").
func componentList(components []string) string {
	quoted := make([]string, len(components))
	for i, c := range components {
		quoted[i] = `"` + c + `"`
	}
	return "(" + strings.Join(quoted, " ") + ")"
}

// parseInput splits a Signature-Input member value, e.g.
// ("authorization" "atproto-space-audience");tag="atproto-space-credential",
// into its covered component list (as written) and its parameters. Parameter
// values are unquoted; a bare key parameter maps to "".
func parseInput(input string) (string, map[string]string, error) {
	end := strings.IndexByte(input, ')')
	if !strings.HasPrefix(input, "(") || end < 0 {
		return "", nil, fmt.Errorf("%w: malformed Signature-Input", ErrInvalidSignature)
	}
	covered := input[:end+1]
	params := map[string]string{}
	rest := input[end+1:]
	for rest != "" {
		var ok bool
		if rest, ok = strings.CutPrefix(rest, ";"); !ok {
			return "", nil, fmt.Errorf("%w: malformed Signature-Input", ErrInvalidSignature)
		}
		name, value, next, err := parseParam(rest)
		if err != nil {
			return "", nil, err
		}
		params[name] = value
		rest = next
	}
	return covered, params, nil
}

// parseParam reads one key[=value] parameter off the front of s, returning
// it and the remainder of s. value is a quoted string, unquoted, or a bare
// token or integer.
func parseParam(s string) (name, value, rest string, err error) {
	i := strings.IndexAny(s, "=;")
	if i < 0 {
		return s, "", "", nil
	}
	name = s[:i]
	if s[i] == ';' {
		return name, "", s[i:], nil
	}
	s = s[i+1:]
	if !strings.HasPrefix(s, `"`) {
		j := strings.IndexByte(s, ';')
		if j < 0 {
			return name, s, "", nil
		}
		return name, s[:j], s[j:], nil
	}
	var b strings.Builder
	for j := 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			if j+1 < len(s) {
				j++
				b.WriteByte(s[j])
			}
		case '"':
			return name, b.String(), s[j+1:], nil
		default:
			b.WriteByte(s[j])
		}
	}
	return "", "", "", fmt.Errorf("%w: unterminated string in Signature-Input", ErrInvalidSignature)
}

// dictMember finds label's value in a Structured Field Dictionary spread over
// one or more header lines, e.g. `sig1=(...), atproto-space-credential=(...)`.
// Members are split on commas outside quoted strings and inner lists.
func dictMember(lines []string, label string) (string, bool) {
	for _, line := range lines {
		depth, quoted, start := 0, false, 0
		for i := 0; i <= len(line); i++ {
			if i < len(line) {
				switch c := line[i]; {
				case quoted && c == '\\':
					i++
					continue
				case c == '"':
					quoted = !quoted
					continue
				case quoted:
					continue
				case c == '(':
					depth++
					continue
				case c == ')':
					depth--
					continue
				case c != ',' || depth > 0:
					continue
				}
			}
			member := strings.TrimSpace(line[start:i])
			start = i + 1
			if v, ok := strings.CutPrefix(member, label+"="); ok {
				return v, true
			}
		}
	}
	return "", false
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
