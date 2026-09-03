package clientmetadata_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/clientmetadata"
	clientmetadata_testutil "github.com/habitat-network/habitat/internal/clientmetadata/testutil"
)

const testSpaceOwner = syntax.DID("did:plc:owner")

func TestVerifyAttestationValid(t *testing.T) {
	clientID, priv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
	raw := clientmetadata_testutil.SignAttestation(t, priv, "key-1", clientID, testSpaceOwner, nil)

	got, err := clientmetadata.VerifyAttestation(
		context.Background(),
		clientmetadata.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.NoError(t, err)
	require.Equal(t, clientID, got)
}

func TestVerifyAttestationRejects(t *testing.T) {
	cases := map[string]func(*jwt.RegisteredClaims, map[string]any){
		"wrong typ": func(_ *jwt.RegisteredClaims, header map[string]any) {
			header["typ"] = "jwt"
		},
		"iss != sub": func(c *jwt.RegisteredClaims, _ map[string]any) {
			c.Subject = "https://other.example.com/client-metadata.json"
		},
		"wrong aud": func(c *jwt.RegisteredClaims, _ map[string]any) {
			c.Audience = jwt.ClaimStrings{"did:plc:someone-else#atproto_space_host"}
		},
		"expired": func(c *jwt.RegisteredClaims, _ map[string]any) {
			c.IssuedAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
		},
		"missing jti": func(c *jwt.RegisteredClaims, _ map[string]any) {
			c.ID = ""
		},
		"exp far beyond max ttl": func(c *jwt.RegisteredClaims, _ map[string]any) {
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(24 * time.Hour))
		},
		"iat and exp both shifted far into the future": func(c *jwt.RegisteredClaims, _ map[string]any) {
			// A valid exp-iat delta (30s), but both shifted 24h into the
			// future: jwt.WithIssuedAt (attestationLeeway) rejects the iat
			// outright, so this never reaches the exp-iat delta check.
			c.IssuedAt = jwt.NewNumericDate(time.Now().Add(24 * time.Hour))
			c.ExpiresAt = jwt.NewNumericDate(time.Now().Add(24*time.Hour + 30*time.Second))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			clientID, priv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
			raw := clientmetadata_testutil.SignAttestation(
				t, priv, "key-1", clientID, testSpaceOwner, mutate,
			)

			_, err := clientmetadata.VerifyAttestation(
				context.Background(),
				clientmetadata.NewResolver(),
				raw,
				testSpaceOwner,
			)
			require.ErrorIs(t, err, clientmetadata.ErrInvalidAttestation)
		})
	}
}

func TestVerifyAttestationRejectsBadSignature(t *testing.T) {
	clientID, _ := clientmetadata_testutil.AttestationTestClient(t, "key-1")
	otherPriv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	// Signed by a key that doesn't match the one published at clientID.
	raw := clientmetadata_testutil.SignAttestation(
		t,
		otherPriv,
		"key-1",
		clientID,
		testSpaceOwner,
		nil,
	)

	_, err = clientmetadata.VerifyAttestation(
		context.Background(),
		clientmetadata.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.ErrorIs(t, err, clientmetadata.ErrInvalidAttestation)
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

	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	raw := clientmetadata_testutil.SignAttestation(
		t,
		priv,
		"key-1",
		unreachable,
		testSpaceOwner,
		nil,
	)

	_, err = clientmetadata.VerifyAttestation(
		context.Background(),
		clientmetadata.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.ErrorIs(t, err, clientmetadata.ErrInvalidAttestation)
	require.NotContains(t, err.Error(), "connection refused")
	require.Contains(t, err.Error(), "unable to resolve client key")
}

func TestVerifyAttestationRejectsUnknownKid(t *testing.T) {
	clientID, priv := clientmetadata_testutil.AttestationTestClient(t, "key-1")
	raw := clientmetadata_testutil.SignAttestation(t, priv, "key-2", clientID, testSpaceOwner, nil)

	_, err := clientmetadata.VerifyAttestation(
		context.Background(),
		clientmetadata.NewResolver(),
		raw,
		testSpaceOwner,
	)
	require.ErrorIs(t, err, clientmetadata.ErrInvalidAttestation)
}
