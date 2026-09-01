package login

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

var testSigningSecret = []byte("test-signing-secret-for-org-00000")

func newTestLoginProvider(t *testing.T) *PasswordLoginProvider {
	t.Helper()
	provider, err := NewPasswordProvider(
		testutil.NewDB(t),
		"pear.example.com",
		testSigningSecret,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
	)
	require.NoError(t, err)
	return provider
}

func TestLoginProvider_Authorize(t *testing.T) {
	p := newTestLoginProvider(t)
	redirect, state, err := p.Authorize(context.Background(), "did:web:alice.example.com")
	require.NoError(t, err)
	require.Nil(t, state)
	// loginHint is resolved from a DID to a handle (from the dummy directory)
	// so the login page can display something readable.
	require.Equal(
		t,
		"https://pear.example.com/ui/login/habitat?handle=example.handle.com",
		redirect,
	)
}

func TestLoginProvider_Authorize_EmptyLoginHint(t *testing.T) {
	p := newTestLoginProvider(t)
	redirect, state, err := p.Authorize(context.Background(), "")
	require.NoError(t, err)
	require.Nil(t, state)
	require.Equal(t, "https://pear.example.com/ui/login/habitat?handle=", redirect)
}

func TestLoginProvider_ExchangeRoundTrip(t *testing.T) {
	p := newTestLoginProvider(t)
	did := syntax.DID("did:web:alice.example.com")
	token, err := p.issueToken(did)
	require.NoError(t, err)
	require.NotEmpty(t, token)
	loginID, err := p.Exchange(context.Background(), url.Values{"code": {token}}, nil)
	require.NoError(t, err)
	require.Equal(t, did.String(), loginID)
}

func TestLoginProvider_Exchange_InvalidToken(t *testing.T) {
	p := newTestLoginProvider(t)
	_, err := p.Exchange(context.Background(), url.Values{"code": {"not-a-jwt"}}, nil)
	require.ErrorIs(t, err, errInvalidLoginToken)
}

func TestLoginProvider_Exchange_WrongSigningSecret(t *testing.T) {
	p := newTestLoginProvider(t)

	// Token signed with a different secret should fail verification.
	other := []byte("a-different-secret-for-this-test")
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: other}, nil)
	require.NoError(t, err)
	claims := jwt.Claims{Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute))}
	tok, err := jwt.Signed(sig).Claims(claims).CompactSerialize()
	require.NoError(t, err)

	_, err = p.Exchange(context.Background(), url.Values{"code": {tok}}, nil)
	require.ErrorIs(t, err, errInvalidLoginToken)
}

func TestLoginProvider_Exchange_ExpiredToken(t *testing.T) {
	p := newTestLoginProvider(t)

	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: p.signingSecret}, nil)
	require.NoError(t, err)
	claims := jwt.Claims{Expiry: jwt.NewNumericDate(time.Now().Add(-time.Minute))}
	tok, err := jwt.Signed(sig).Claims(claims).CompactSerialize()
	require.NoError(t, err)

	_, err = p.Exchange(context.Background(), url.Values{"code": {tok}}, nil)
	require.ErrorIs(t, err, errInvalidLoginToken)
}

func TestLoginProvider_HandlePasswordLogin_Success(t *testing.T) {
	p := newTestLoginProvider(t)
	err := p.AddLoginEntry("did:web:example.did.com", "12345")
	require.NoError(t, err)
	body, _ := json.Marshal(habitat.NetworkHabitatOrgLoginMemberInput{
		Handle:   "alice.example.com",
		Password: "12345",
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgLoginMemberOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))

	callbackUrl, err := url.Parse(out.CallbackURL)
	require.NoError(t, err, "failed to parse callback URL")

	require.Equal(t, callbackUrl.Scheme, "https")
	require.Equal(t, callbackUrl.Host, "pear.example.com")
	require.Equal(t, callbackUrl.Path, "/oauth-callback")

	code := callbackUrl.Query().Get("code")
	loginID, err := p.Exchange(t.Context(), url.Values{"code": {code}}, nil)
	require.NoError(t, err)
	// from dummy directory
	require.Equal(t, "did:web:example.did.com", loginID)
}

func TestLoginProvider_HandlePasswordLogin_BadRequestBody(t *testing.T) {
	p := newTestLoginProvider(t)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLoginProvider_HandlePasswordLogin_WrongPassword(t *testing.T) {
	p := newTestLoginProvider(t)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgLoginMemberInput{
		Handle:   "alice.testorg.example.com",
		Password: "wrong-password",
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHashPassword_VerifySamePassword(t *testing.T) {
	hash, err := hashPassword("correct-horse-battery-staple")
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	ok, err := verifyPassword("correct-horse-battery-staple", hash)
	require.NoError(t, err)
	require.True(t, ok)
}

func TestVerifyPassword_WrongPassword(t *testing.T) {
	hash, err := hashPassword("correct-horse-battery-staple")
	require.NoError(t, err)

	ok, err := verifyPassword("wrong-password", hash)
	require.NoError(t, err)
	require.False(t, ok)
}

func TestHashPassword_UniqueHashes(t *testing.T) {
	// Two hashes of the same password must differ (unique salts)
	hash1, err := hashPassword("same-password")
	require.NoError(t, err)
	hash2, err := hashPassword("same-password")
	require.NoError(t, err)
	require.NotEqual(t, hash1, hash2)
}
