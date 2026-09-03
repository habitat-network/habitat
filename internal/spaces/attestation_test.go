package spaces_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/clientmeta"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

const testSpaceOwner = syntax.DID("did:plc:owner")

func TestVerifyAttestationValid(t *testing.T) {
	clientID, priv := spaces_testutil.AttestationTestClient(t, "key-1")
	raw := spaces_testutil.SignAttestation(t, priv, "key-1", clientID, testSpaceOwner, nil)

	got, err := spaces.VerifyAttestation(
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
			clientID, priv := spaces_testutil.AttestationTestClient(t, "key-1")
			raw := spaces_testutil.SignAttestation(
				t, priv, "key-1", clientID, testSpaceOwner, mutate,
			)

			_, err := spaces.VerifyAttestation(
				context.Background(),
				clientmeta.NewResolver(),
				raw,
				testSpaceOwner,
			)
			require.ErrorIs(t, err, spaces.ErrInvalidAttestation)
		})
	}
}

func TestVerifyAttestationRejectsBadSignature(t *testing.T) {
	clientID, _ := spaces_testutil.AttestationTestClient(t, "key-1")
	otherPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	// Signed by a key that doesn't match the one published at clientID.
	raw := spaces_testutil.SignAttestation(t, otherPriv, "key-1", clientID, testSpaceOwner, nil)

	_, err = spaces.VerifyAttestation(
		context.Background(),
		clientmeta.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.ErrorIs(t, err, spaces.ErrInvalidAttestation)
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
	raw := spaces_testutil.SignAttestation(t, priv, "key-1", unreachable, testSpaceOwner, nil)

	_, err = spaces.VerifyAttestation(
		context.Background(),
		clientmeta.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.ErrorIs(t, err, spaces.ErrInvalidAttestation)
	require.NotContains(t, err.Error(), "connection refused")
	require.Contains(t, err.Error(), "unable to resolve client key")
}

func TestVerifyAttestationRejectsUnknownKid(t *testing.T) {
	clientID, priv := spaces_testutil.AttestationTestClient(t, "key-1")
	raw := spaces_testutil.SignAttestation(t, priv, "key-2", clientID, testSpaceOwner, nil)

	_, err := spaces.VerifyAttestation(
		context.Background(),
		clientmeta.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.ErrorIs(t, err, spaces.ErrInvalidAttestation)
}
