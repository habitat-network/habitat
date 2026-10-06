package authn_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
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

func TestSpaceCredentialAuthMethod(t *testing.T) {
	hostKey, _ := atcrypto.GeneratePrivateKeyK256()
	hostPubKey, _ := hostKey.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(
		*did.Web("pear.com").AtprotoKey(hostPubKey.Multibase()).ATProtoSpaceKey(hostPubKey.Multibase()).Build(),
	)
	method := authn.NewSpaceCredentialAuthMethod(dir)

	t.Run("atproto_space", func(t *testing.T) {
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto_space",
			"at://did:web:pear.com/space/com.test.space/abc",
		)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
		require.True(t, ok)
		require.Equal(t, credInfo.Space.String(), "at://did:web:pear.com/space/com.test.space/abc")
		require.Empty(t, credInfo.Subject)
		require.Equal(t, authn.ValidatorMethodSpaceCredential, credInfo.Method)
	})

	t.Run("atproto", func(t *testing.T) {
		token, err := utils.SpaceCredential(
			hostKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
		)
		require.NoError(t, err)
		r := newAuthenticatedRequest(token)
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
		token, _ := utils.SpaceCredential(
			otherKey,
			"#atproto",
			"at://did:web:pear.com/space/com.test.space/abc",
		)
		r := newAuthenticatedRequest(token)
		require.True(t, method.CanHandle(r))
		credInfo, ok := method.Validate(httptest.NewRecorder(), r)
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

	const space = "at://did:web:pear.com/space/com.test.space/abc"

	t.Run("expires in 10 minutes with a random jti", func(t *testing.T) {
		claimsOf := func(token string) jwt.MapClaims {
			claims := jwt.MapClaims{}
			_, _, err := jwt.NewParser().ParseUnverified(token, claims)
			require.NoError(t, err)
			return claims
		}
		a, err := utils.SpaceCredential(hostKey, "#atproto_space", space)
		require.NoError(t, err)
		b, err := utils.SpaceCredential(hostKey, "#atproto_space", space)
		require.NoError(t, err)
		ca, cb := claimsOf(a), claimsOf(b)
		require.InDelta(t, ca["iat"], ca["exp"].(float64)-600, 0)
		require.NotEmpty(t, ca["jti"])
		require.NotEqual(t, ca["jti"], cb["jti"])
	})

	t.Run("still accepts an older one hour credential", func(t *testing.T) {
		now := time.Now()
		token, err := new(jwt.Token{
			Method: jwt.GetSigningMethod("ES256K"),
			Claims: jwt.MapClaims{
				"iss": "did:web:pear.com",
				"sub": space,
				"iat": jwt.NewNumericDate(now),
				"exp": jwt.NewNumericDate(now.Add(time.Hour)),
				"jti": "legacy",
			},
			Header: map[string]any{
				"typ": "atproto-space-credential+jwt", "kid": "#atproto_space", "alg": "ES256K",
			},
		}).SignedString(hostKey)
		require.NoError(t, err)
		_, ok := method.Validate(httptest.NewRecorder(), newAuthenticatedRequest(token))
		require.True(t, ok)
	})
}
