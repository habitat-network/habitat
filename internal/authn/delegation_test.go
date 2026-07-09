package authn

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/pdsclient"
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
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, NewDelegationTokenAuthMethod(nil, nil).CanHandle(r))
}

func TestDelegationAuthMethod_Validate(t *testing.T) {
	dir := pdsclient.NewDummyDirectory("https://pds.com")
	spaceURI := habitat_syntax.SpaceURI("ats://did:web:example.com/test.space.type/abc")
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
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		credInfo, ok := NewDelegationTokenAuthMethod(dir, fga).Validate(w, r)
		require.True(t, ok)
		require.Equal(t, credInfo, &CredentialInfo{Space: spaceURI})
	})

	t.Run("no permission", func(t *testing.T) {
		fga, err := fgastore.NewMemory(t.Context())
		require.NoError(t, err)
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Authorization", "Bearer "+token)
		_, ok := NewDelegationTokenAuthMethod(dir, fga).Validate(w, r)
		require.False(t, ok)
	})
}
