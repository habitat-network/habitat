package testutil

import (
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

	"github.com/habitat-network/habitat/internal/spaces"
)

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
	extra := map[jose.HeaderKey]any{jose.HeaderType: spaces.AttestationTyp}
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
