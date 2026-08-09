package authn

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/fgastore"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/stretchr/testify/require"
)

func TestDelegationAuthMethod_CanHandle(t *testing.T) {
	token, err := new(jwt.Token{
		Header: map[string]any{
			"typ": "atproto-space-delegation+jwt",
			"alg": "HS256",
		},
		Method: jwt.SigningMethodHS256,
	}).SignedString([]byte("secret"))
	require.NoError(t, err)
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, NewDelegationTokenAuthMethod(nil, nil, nil).CanHandle(r))
}

func TestDelegationAuthMethod_Validate(t *testing.T) {
	key, _ := atcrypto.GeneratePrivateKeyK256()
	publicKey, _ := key.PublicKey()
	dir := identity.NewMockDirectory()
	dir.Insert(*did.Web("example.com").AtprotoKey(publicKey.Multibase()).Build())
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
	}).SignedString(key)
	require.NoError(t, err)
	t.Run("has permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		require.NoError(t, fga.Write(
			t.Context(),
			fgastore.MemberUserString(syntax.DID("did:web:example.com")),
			fgastore.RelationSpaceMemberManager,
			fgastore.SpaceObjectKey(spaceURI),
		))
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer "+token)
		credInfo, ok := NewDelegationTokenAuthMethod(dir, fga, nil).Validate(w, r)
		require.True(t, ok)
		require.Equal(t, credInfo, &CredentialInfo{Space: spaceURI})
	})

	t.Run("no permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer "+token)
		_, ok := NewDelegationTokenAuthMethod(dir, fga, nil).Validate(w, r)
		require.False(t, ok)
	})
}

func TestDelegationAuthMethod_Validate_HostToken(t *testing.T) {
	key, _ := atcrypto.GeneratePrivateKeyK256()
	dir := identity.NewMockDirectory()
	spaceURI := habitat_syntax.SpaceURI("at://did:web:example.com/space/test.space.type/abc")
	token, err := new(jwt.Token{
		Header: map[string]any{
			"typ": "atproto-space-credential+jwt",
			"alg": "ES256K",
			"kid": "#habitat",
		},
		Claims: jwt.MapClaims{
			"iss": "did:web:example.com",
			"sub": spaceURI,
			"exp": jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
		Method: jwt.GetSigningMethod("ES256K"),
	}).SignedString(key)
	require.NoError(t, err)
	t.Run("has permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		require.NoError(t, fga.Write(
			t.Context(),
			fgastore.MemberUserString(syntax.DID("did:web:example.com")),
			fgastore.RelationSpaceMemberManager,
			fgastore.SpaceObjectKey(spaceURI),
		))
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer "+token)
		credInfo, ok := NewDelegationTokenAuthMethod(dir, fga, key).Validate(w, r)
		require.True(t, ok)
		require.Equal(t, credInfo, &CredentialInfo{Space: spaceURI})
	})

	t.Run("no permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", http.NoBody)
		r.Header.Set("Authorization", "Bearer "+token)
		_, ok := NewDelegationTokenAuthMethod(dir, fga, key).Validate(w, r)
		require.False(t, ok)
	})
}
