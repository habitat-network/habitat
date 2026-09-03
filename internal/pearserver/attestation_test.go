package pearserver

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/clientmeta"
)

const testSpaceOwner = syntax.DID("did:plc:owner")

// AttestationTestClient generates a P-256 keypair, serves a
// client-metadata.json embedding its public key (as an atcrypto JWK, under
// kid) at a freshly started test server, and returns the resulting client_id
// (usable as both the served URL and the attestation's iss/sub) plus the
// private key for signing test attestations.
func AttestationTestClient(t *testing.T, kid string) (clientID string, priv *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	jwk := atcrypto.JWK{
		KeyType: "EC",
		Curve:   "P-256",
		//nolint:staticcheck // SA1019: deprecated ecdsa.PublicKey X/Y fields, same as internal/clientmeta.ConvertJWK
		X: base64.RawURLEncoding.EncodeToString(priv.PublicKey.X.Bytes()),
		//nolint:staticcheck // SA1019: deprecated ecdsa.PublicKey X/Y fields, same as internal/clientmeta.ConvertJWK
		Y:     base64.RawURLEncoding.EncodeToString(priv.PublicKey.Y.Bytes()),
		KeyID: &kid,
	}

	var url string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: url,
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
		}))
	}))
	t.Cleanup(server.Close)
	url = server.URL + "/client-metadata.json"
	return url, priv
}

// SignAttestation builds and signs a client attestation JWT for clientID,
// overriding fields via mutate for negative-path tests.
func SignAttestation(
	t *testing.T,
	priv *ecdsa.PrivateKey,
	kid string,
	clientID string,
	spaceOwner syntax.DID,
	mutate func(*josejwt.Claims, map[jose.HeaderKey]any),
) string {
	t.Helper()
	extra := map[jose.HeaderKey]any{jose.HeaderType: attestationTyp}
	claims := &josejwt.Claims{
		Issuer:   clientID,
		Subject:  clientID,
		Audience: josejwt.Audience{spaceOwner.String() + "#atproto_space_host"},
		IssuedAt: josejwt.NewNumericDate(time.Now()),
		Expiry:   josejwt.NewNumericDate(time.Now().Add(30 * time.Second)),
		ID:       "test-nonce",
	}
	if mutate != nil {
		mutate(claims, extra)
	}
	so := &jose.SignerOptions{ExtraHeaders: extra}
	so.WithHeader("kid", kid)
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: priv}, so)
	require.NoError(t, err)
	raw, err := josejwt.Signed(signer).Claims(claims).CompactSerialize()
	require.NoError(t, err)
	return raw
}

func TestVerifyAttestationValid(t *testing.T) {
	clientID, priv := AttestationTestClient(t, "key-1")
	raw := SignAttestation(t, priv, "key-1", clientID, testSpaceOwner, nil)

	got, err := verifyAttestation(
		context.Background(),
		clientmeta.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.NoError(t, err)
	require.Equal(t, clientID, got)
}

func TestVerifyAttestationRejects(t *testing.T) {
	cases := map[string]func(*josejwt.Claims, map[jose.HeaderKey]any){
		"wrong typ": func(_ *josejwt.Claims, extra map[jose.HeaderKey]any) {
			extra[jose.HeaderType] = "jwt"
		},
		"iss != sub": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.Subject = "https://other.example.com/client-metadata.json"
		},
		"wrong aud": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.Audience = josejwt.Audience{"did:plc:someone-else#atproto_space_host"}
		},
		"expired": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.IssuedAt = josejwt.NewNumericDate(time.Now().Add(-time.Hour))
			c.Expiry = josejwt.NewNumericDate(time.Now().Add(-time.Minute))
		},
		"missing jti": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.ID = ""
		},
		"exp far beyond max ttl": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.Expiry = josejwt.NewNumericDate(time.Now().Add(24 * time.Hour))
		},
		"iat and exp both shifted far into the future": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			// A valid exp-iat delta (30s), but both shifted 24h into the
			// future: this must be rejected on absolute time, not just the
			// exp-iat delta, or an attacker can mint an attestation that's
			// "fresh" by relative-delta standards but never actually expires
			// close to now.
			c.IssuedAt = josejwt.NewNumericDate(time.Now().Add(24 * time.Hour))
			c.Expiry = josejwt.NewNumericDate(time.Now().Add(24*time.Hour + 30*time.Second))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			clientID, priv := AttestationTestClient(t, "key-1")
			raw := SignAttestation(t, priv, "key-1", clientID, testSpaceOwner, mutate)

			_, err := verifyAttestation(
				context.Background(),
				clientmeta.NewResolver(),
				raw,
				testSpaceOwner,
			)
			require.ErrorIs(t, err, ErrInvalidAttestation)
		})
	}
}

func TestVerifyAttestationRejectsBadSignature(t *testing.T) {
	clientID, _ := AttestationTestClient(t, "key-1")
	otherPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	// Signed by a key that doesn't match the one published at clientID.
	raw := SignAttestation(t, otherPriv, "key-1", clientID, testSpaceOwner, nil)

	_, err = verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
	require.ErrorIs(t, err, ErrInvalidAttestation)
}

// TestVerifyAttestationRejectsUnreachableIssuer covers the SSRF-oracle
// finding: a resolver-fetch failure for an attacker-controlled iss must
// become a generic InvalidClientAttestation rejection, not leak the raw
// fetch error (which would let a caller distinguish connection-refused vs.
// DNS failure vs. non-200 vs. malformed JSON for arbitrary URLs).
func TestVerifyAttestationRejectsUnreachableIssuer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	unreachable := server.URL + "/client-metadata.json"
	server.Close() // closed: any request now fails with connection refused

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	raw := SignAttestation(t, priv, "key-1", unreachable, testSpaceOwner, nil)

	_, err = verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
	require.ErrorIs(t, err, ErrInvalidAttestation)
	require.NotContains(t, err.Error(), "connection refused")
	require.Contains(t, err.Error(), "unable to resolve client key")
}

func TestVerifyAttestationRejectsUnknownKid(t *testing.T) {
	clientID, priv := AttestationTestClient(t, "key-1")
	raw := SignAttestation(t, priv, "key-2", clientID, testSpaceOwner, nil)

	_, err := verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
	require.ErrorIs(t, err, ErrInvalidAttestation)
}
