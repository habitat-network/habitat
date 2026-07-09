package authn

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
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
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, NewSpaceCredentialAuthMethod(nil).CanHandle(r))
}

func TestSpaceCredentialAuthMethod_Validate(t *testing.T) {
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
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	credInfo, ok := NewSpaceCredentialAuthMethod(dir).Validate(w, r)
	require.True(t, ok)
	require.Equal(t, credInfo, &CredentialInfo{
		Space: spaceURI,
	})
}
