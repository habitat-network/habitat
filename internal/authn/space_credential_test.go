package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/pdsclient"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

func TestSpaceCredentialAuthMethod_CanHandle(t *testing.T) {
	token, err := new(jwt.Token{
		Header: map[string]any{
			"typ": "atproto-space-credential+jwt",
			"alg": "HS256",
		},
		Method: jwt.SigningMethodHS256,
	}).SignedString([]byte("secret"))
	require.NoError(t, err)
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, NewSpaceCredentialAuthMethod(nil, nil).CanHandle(r))
}

func TestSpaceCredentialAuthMethod_Validate(t *testing.T) {
	dir := pdsclient.NewDummyDirectory("https://pds.com")
	spaceURI := habitat_syntax.SpaceURI("at://did:web:example.com/space/test.space.type/abc")
	token, err := new(jwt.Token{
		Header: map[string]any{
			"typ": "atproto-space-credential+jwt",
			"alg": "ES256K",
			"kid": "#atproto",
		},
		Claims: jwt.MapClaims{
			"iss": "did:web:example.com",
			"sub": spaceURI,
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Method: jwt.GetSigningMethod("ES256K"),
	}).SignedString(dir.PrivateKey)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)

	credInfo, ok := NewSpaceCredentialAuthMethod(dir, nil).Validate(w, r)
	require.True(t, ok)
	require.Equal(t, credInfo, &CredentialInfo{
		Space: spaceURI,
	})
}

func TestSpaceCredentialAuthMethod_Validate_InvalidToken(t *testing.T) {
	key, _ := atcrypto.GeneratePrivateKeyK256()
	publicKey, _ := key.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(*did.Web("example.com").AtprotoKey(publicKey.Multibase()).Build())
	method := NewSpaceCredentialAuthMethod(dir, nil)

	t.Run("no token", func(t *testing.T) {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", http.NoBody)
		_, ok := method.Validate(w, r)
		require.False(t, ok)
	})

	t.Run("space issuer mismatch", func(t *testing.T) {
		token, _ := new(jwt.Token{
			Header: map[string]any{
				"typ": "atproto-space-credential+jwt",
				"alg": "ES256K",
				"kid": "#atproto",
			},
			Claims: jwt.MapClaims{
				"iss": "did:web:example.com",
				"sub": "at://did:web:other.example.com/space/test.space.type/abc",
				"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
			},
			Method: jwt.GetSigningMethod("ES256K"),
		}).SignedString(publicKey)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer "+token)
		_, ok := method.Validate(w, r)
		require.False(t, ok)
	})
}
