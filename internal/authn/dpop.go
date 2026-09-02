package authn

import (
	"crypto"
	"crypto/ecdsa"
	"encoding/base64"
	"fmt"
	"net/http"
	"sync"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/utils"
)

// dpopIatFreshness bounds how old (or how far in the future) a DPoP proof's
// `iat` claim may be, per RFC 9449 §11.1.
const dpopIatFreshness = 60 * time.Second

// dpopReplayStore rejects a DPoP proof `jti` it has already seen within
// dpopIatFreshness of its `iat`, preventing replay of a captured proof (RFC
// 9449 §11.1). Entries are pruned lazily as new proofs come in.
type dpopReplayStore struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func newDPoPReplayStore() *dpopReplayStore {
	return &dpopReplayStore{seen: make(map[string]time.Time)}
}

// claim records jti as seen, expiring at expiry. It returns false if jti was
// already recorded and has not yet expired.
func (s *dpopReplayStore) claim(jti string, expiry time.Time) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	for id, exp := range s.seen {
		if now.After(exp) {
			delete(s.seen, id)
		}
	}
	if exp, ok := s.seen[jti]; ok && now.Before(exp) {
		return false
	}
	s.seen[jti] = expiry
	return true
}

// verifyDPoPProof verifies the DPoP proof JWT carried in r's "DPoP" header
// against r itself, per RFC 9449 §4.3: proof signature against its own
// embedded `jwk` header, `htm`/`htu` matching the request, a fresh `iat`,
// and an unreplayed `jti`. When accessToken is non-empty, the proof's `ath`
// claim must match the token's hash, binding the proof to that specific
// token (RFC 9449 §4.3 point 9); pass "" when the proof merely establishes a
// key for a token about to be minted, as in space-credential issuance.
//
// On success it returns the RFC 7638 JWK thumbprint of the proof's key, so
// callers can bind or check it against a credential's `cnf.jkt` claim.
func verifyDPoPProof(r *http.Request, replay *dpopReplayStore, accessToken string) (string, error) {
	proof := r.Header.Get("DPoP")
	if proof == "" {
		return "", fmt.Errorf("missing DPoP proof")
	}
	token, err := josejwt.ParseSigned(proof)
	if err != nil {
		return "", fmt.Errorf("parse DPoP proof: %w", err)
	}
	if len(token.Headers) != 1 {
		return "", fmt.Errorf("DPoP proof must have exactly one signature")
	}
	header := token.Headers[0]
	if typ, _ := header.ExtraHeaders[jose.HeaderKey("typ")].(string); typ != "dpop+jwt" {
		return "", fmt.Errorf("DPoP proof has wrong typ")
	}
	jwk := header.JSONWebKey
	if jwk == nil {
		return "", fmt.Errorf("DPoP proof missing jwk header")
	}
	if _, ok := jwk.Key.(*ecdsa.PublicKey); !ok {
		return "", fmt.Errorf("DPoP proof jwk must be an EC public key")
	}
	var claims utils.DPoPProofClaims
	if err := token.Claims(jwk.Key, &claims); err != nil {
		return "", fmt.Errorf("verify DPoP proof signature: %w", err)
	}
	if claims.ID == "" {
		return "", fmt.Errorf("DPoP proof missing jti")
	}
	if claims.Method != r.Method {
		return "", fmt.Errorf("DPoP proof htm does not match request")
	}
	htu, err := utils.DPoPProofURL(requestURL(r))
	if err != nil {
		return "", fmt.Errorf("build request url: %w", err)
	}
	if claims.URL != htu {
		return "", fmt.Errorf("DPoP proof htu does not match request")
	}
	if claims.IssuedAt == nil {
		return "", fmt.Errorf("DPoP proof missing iat")
	}
	iat := claims.IssuedAt.Time()
	now := time.Now()
	if iat.After(now.Add(10*time.Second)) || now.Sub(iat) > dpopIatFreshness {
		return "", fmt.Errorf("DPoP proof iat is not fresh")
	}
	if accessToken != "" && claims.AccessTokenHash != utils.HashDPoPToken(accessToken) {
		return "", fmt.Errorf("DPoP proof ath does not match presented token")
	}
	if !replay.claim(claims.ID, iat.Add(dpopIatFreshness)) {
		return "", fmt.Errorf("DPoP proof jti has already been used")
	}
	thumb, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("compute jwk thumbprint: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(thumb), nil
}

// requestURL reconstructs the absolute URL (without query or fragment) that
// the caller must have used as this request's `htu`. Habitat always sits
// behind a TLS-terminating proxy (Caddy, ngrok) in every environment that
// accepts DPoP-bound credentials, so the scheme is https unless the proxy
// says otherwise.
func requestURL(r *http.Request) string {
	scheme := "https"
	if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
		scheme = proto
	} else if r.TLS == nil {
		scheme = "http"
	}
	return scheme + "://" + r.Host + r.URL.Path
}
