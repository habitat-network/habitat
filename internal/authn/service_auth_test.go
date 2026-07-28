package authn

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

func TestServiceAuthValidate(t *testing.T) {
	directory := pdsclient.NewDummyDirectory("https://pds.com")
	lxm := syntax.NSID("io.example.test")
	token, err := auth.SignServiceAuth(
		syntax.DID("did:plc:test"),
		"https://pds.com",
		time.Hour,
		&lxm,
		directory.PrivateKey,
	)
	require.NoError(t, err)
	serviceAuth := NewServiceAuthMethod(directory, "https://pds.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/xrpc/io.example.test", nil)
	r.Header.Set("Authorization", "Bearer "+token)

	credInfo, ok := serviceAuth.Validate(w, r)

	require.True(t, ok)
	require.Equal(t, syntax.DID("did:plc:test"), credInfo.Subject)
}

func TestServiceAuthValidate_InvalidToken(t *testing.T) {
	directory := pdsclient.NewDummyDirectory("https://pds.com")
	serviceAuth := NewServiceAuthMethod(directory, "https://pds.com")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/xrpc/lxm", nil)
	r.Header.Set("Authorization", "Bearer invalid")
	_, ok := serviceAuth.Validate(w, r)
	require.False(t, ok)
}

func TestServiceAuthCanHandle(t *testing.T) {
	directory := pdsclient.NewDummyDirectory("https://pds.com")
	tok := jwt.NewWithClaims(jwt.GetSigningMethod("ES256K"), jwt.MapClaims{
		"iss": "did:plc:test",
		"aud": "https://pds.com",
	})
	tok.Header["kid"] = "#atproto"
	token, err := tok.SignedString(directory.PrivateKey)
	require.NoError(t, err)
	serviceAuth := NewServiceAuthMethod(directory, "https://pds.com")
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, serviceAuth.CanHandle(r))
}
