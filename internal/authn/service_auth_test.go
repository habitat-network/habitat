package authn

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/org"
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
	serviceAuth := NewServiceAuthMethod(
		org.NewEveryoneOrg("everyone.example.com"),
		directory,
		FixedAudience("https://pds.com"),
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/xrpc/io.example.test", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)

	credInfo, ok := serviceAuth.Validate(w, r)

	require.True(t, ok)
	require.Equal(t, syntax.DID("did:plc:test"), credInfo.Subject)
}

func TestServiceAuthValidate_InvalidToken(t *testing.T) {
	directory := pdsclient.NewDummyDirectory("https://pds.com")
	serviceAuth := NewServiceAuthMethod(
		org.NewEveryoneOrg("everyone.example.com"),
		directory,
		FixedAudience("https://pds.com"),
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/xrpc/lxm", http.NoBody)
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
	serviceAuth := NewServiceAuthMethod(
		org.NewEveryoneOrg("everyone.example.com"),
		directory,
		FixedAudience("https://pds.com"),
	)
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, serviceAuth.CanHandle(r))
}

// TestServiceAuthCanHandle_NoKid covers a real PDS's own
// com.atproto.server.getServiceAuth response (e.g. bsky.social's, fetched
// via internal/forwarding's Atproto-Proxy handling for a non-hive-hosted
// identity): a standards-compliant service-auth JWT that omits "kid"
// entirely, since ServiceAuthValidator.Validate resolves the signing key
// from "iss" via directory lookup, never from "kid".
func TestServiceAuthCanHandle_NoKid(t *testing.T) {
	directory := pdsclient.NewDummyDirectory("https://pds.com")
	tok := jwt.NewWithClaims(jwt.GetSigningMethod("ES256K"), jwt.MapClaims{
		"iss": "did:plc:test",
		"aud": "https://pds.com",
	})
	token, err := tok.SignedString(directory.PrivateKey)
	require.NoError(t, err)
	serviceAuth := NewServiceAuthMethod(
		org.NewEveryoneOrg("everyone.example.com"),
		directory,
		FixedAudience("https://pds.com"),
	)
	r := httptest.NewRequest("GET", "/", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)
	require.True(t, serviceAuth.CanHandle(r))
}

// TestServiceAuthValidate_AudienceRejected covers a multi-tenant instance
// (isValidAudience checks something dynamic, e.g. hive.PrivateKeyForDID —
// see cmd/pear/main.go): a token whose own "aud" claim doesn't pass that
// check must be rejected before any signature verification, even though the
// token is otherwise well-formed and validly signed.
func TestServiceAuthValidate_AudienceRejected(t *testing.T) {
	directory := pdsclient.NewDummyDirectory("https://pds.com")
	lxm := syntax.NSID("io.example.test")
	token, err := auth.SignServiceAuth(
		syntax.DID("did:plc:test"),
		"did:web:untrusted.example#habitat",
		time.Hour,
		&lxm,
		directory.PrivateKey,
	)
	require.NoError(t, err)
	serviceAuth := NewServiceAuthMethod(
		org.NewEveryoneOrg("everyone.example.com"),
		directory,
		func(ctx context.Context, aud string) bool {
			return aud == "did:web:trusted.example#habitat"
		},
	)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/xrpc/io.example.test", http.NoBody)
	r.Header.Set("Authorization", "Bearer "+token)

	_, ok := serviceAuth.Validate(w, r)
	require.False(t, ok)
}
