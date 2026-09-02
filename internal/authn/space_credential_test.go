package authn_test

import (
	"crypto"
	"crypto/ecdsa"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

func newAuthenticatedRequest(token string) *http.Request {
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

// jwkThumbprint returns the RFC 7638 thumbprint of key's public half, in the
// same form a `cnf.jkt` claim carries it.
func jwkThumbprint(t *testing.T, key *ecdsa.PrivateKey) string {
	t.Helper()
	jwk := jose.JSONWebKey{Key: key.Public()}
	thumb, err := jwk.Thumbprint(crypto.SHA256)
	require.NoError(t, err)
	return base64.RawURLEncoding.EncodeToString(thumb)
}

// newSpaceCredentialRequest builds a GET "/" request presenting token as a
// DPoP-bound credential, with a matching DPoP proof signed by key and bound
// to token via the proof's `ath` claim.
func newSpaceCredentialRequest(t *testing.T, token string, key *ecdsa.PrivateKey) *http.Request {
	t.Helper()
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "DPoP "+token)
	proof, err := utils.SignDPoPProof(key, http.MethodGet, "http://"+r.Host+r.URL.Path, token)
	require.NoError(t, err)
	r.Header.Set("DPoP", proof)
	return r
}

func TestSpaceCredentialAuthMethod(t *testing.T) {
	hostKey, _ := atcrypto.GeneratePrivateKeyK256()
	hostPubKey, _ := hostKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(
		*did.Web("pear.com").AtprotoKey(hostPubKey.Multibase()).ATProtoSpaceKey(hostPubKey.Multibase()).Build(),
	)
	method := authn.NewSpaceCredentialAuthMethod(dir)

	t.Run("atproto_space", func(t *testing.T) {
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto_space",
			"at://did:web:pear.com/space/com.test.space/abc",
			jwkThumbprint(t, dpopKey),
		)
		require.NoError(t, err)
		r := newSpaceCredentialRequest(t, token, dpopKey)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, credInfo.Space.String(), "at://did:web:pear.com/space/com.test.space/abc")
		require.Empty(t, credInfo.Subject)
	})

	t.Run("atproto", func(t *testing.T) {
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
			jwkThumbprint(t, dpopKey),
		)
		require.NoError(t, err)
		r := newSpaceCredentialRequest(t, token, dpopKey)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, credInfo.Space.String(), "at://did:web:pear.com/space/com.test.space/abc")
		require.Empty(t, credInfo.Subject)
	})

	t.Run("no token", func(t *testing.T) {
		_, ok := method.Validate(
			httptest.NewRecorder(),
			httptest.NewRequest("GET", "/", http.NoBody),
		)
		require.False(t, ok)
	})

	t.Run("invalid signature", func(t *testing.T) {
		otherKey, _ := atcrypto.GeneratePrivateKeyK256()
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		token, _ := utils.SpaceCredential(
			otherKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
			jwkThumbprint(t, dpopKey),
		)
		r := newSpaceCredentialRequest(t, token, dpopKey)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("missing DPoP proof", func(t *testing.T) {
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
			jwkThumbprint(t, dpopKey),
		)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("DPoP proof key does not match cnf.jkt", func(t *testing.T) {
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		otherDpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
			jwkThumbprint(t, dpopKey),
		)
		require.NoError(t, err)
		r := newSpaceCredentialRequest(t, token, otherDpopKey)
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("DPoP proof replay is rejected", func(t *testing.T) {
		dpopKey, err := utils.GenerateDPoPKey()
		require.NoError(t, err)
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
			jwkThumbprint(t, dpopKey),
		)
		require.NoError(t, err)
		r := newSpaceCredentialRequest(t, token, dpopKey)
		proof := r.Header.Get("DPoP")

		_, ok := method.Validate(httptest.NewRecorder(), r)
		require.True(t, ok)

		replay := httptest.NewRequest("GET", "/", http.NoBody)
		replay.Header.Set("Authorization", "DPoP "+token)
		replay.Header.Set("DPoP", proof)
		credInfo, ok := method.Validate(httptest.NewRecorder(), replay)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})

	t.Run("issuer mismatch", func(t *testing.T) {
		token, _ := new(jwt.Token{
			Header: map[string]any{
				"typ": "atproto-space-credential+jwt",
				"alg": "ES256K",
				"kid": "#atproto",
			},
			Claims: jwt.MapClaims{
				"iss": "did:web:pear.com",
				"sub": "at://did:web:other.example.com/space/test.space.type/abc",
				"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Method: jwt.GetSigningMethod("ES256K"),
		}).SignedString(hostKey)
		r := newAuthenticatedRequest(token)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.False(t, ok)
		require.Nil(t, credInfo)
	})
}
