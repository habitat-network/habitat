package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	_ "github.com/bluesky-social/indigo/atproto/auth" // registers the ES256 signing method used below
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/clientmetadata"
)

// AttestationTestClient generates a P-256 keypair, serves a
// client-metadata.json embedding its public key (as an atcrypto JWK, under
// kid) at a freshly started test server, and returns the resulting client_id
// (usable as both the served URL and the attestation's iss/sub) plus the
// private key for signing test attestations.
func AttestationTestClient(t *testing.T, kid string) (clientID string, priv atcrypto.PrivateKey) {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := key.PublicKey()
	require.NoError(t, err)
	jwk, err := pub.JWK()
	require.NoError(t, err)
	jwk.KeyID = &kid

	var url string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: url,
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{*jwk}},
		}))
	}))
	t.Cleanup(server.Close)
	url = server.URL + "/client-metadata.json"
	return url, key
}

// SignAttestation builds and signs a client attestation JWT for clientID,
// overriding claims or headers via mutate for negative-path tests.
func SignAttestation(
	t *testing.T,
	priv atcrypto.PrivateKey,
	kid string,
	clientID string,
	spaceOwner syntax.DID,
	mutate func(claims *jwt.RegisteredClaims, header map[string]any),
) string {
	t.Helper()
	claims := &jwt.RegisteredClaims{
		Issuer:    clientID,
		Subject:   clientID,
		Audience:  jwt.ClaimStrings{spaceOwner.String() + "#atproto_space_host"},
		IssuedAt:  jwt.NewNumericDate(time.Now()),
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(30 * time.Second)),
		ID:        "test-nonce",
	}
	token := jwt.NewWithClaims(jwt.GetSigningMethod("ES256"), claims)
	token.Header["typ"] = clientmetadata.AttestationTyp
	token.Header["kid"] = kid
	if mutate != nil {
		mutate(claims, token.Header)
	}
	raw, err := token.SignedString(priv)
	require.NoError(t, err)
	return raw
}
