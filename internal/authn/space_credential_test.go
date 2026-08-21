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
}
