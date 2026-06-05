package org

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestLoginProvider(t *testing.T) (*passwordProviderImpl, *orgImpl) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)

	s, err := NewStore(
		db,
		h,
		pdsclient.NewDummyDirectory("https://pds.example.com"),
		"pear.example.com",
	)
	require.NoError(t, err)

	orgId, _, err := s.CreateOrg(t.Context(), "test-org", "admin", "password", "", "", "testorg")
	require.NoError(t, err)

	scoped, err := s.GetOrg(context.Background(), orgId)
	require.NoError(t, err)

	return NewPasswordProvider(
		s,
		"pear.example.com",
		"frontend.example.com",
		testSigningSecret,
		h,
	), scoped.(*orgImpl)
}

func TestLoginProvider_Authorize(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	redirect, state, err := p.Authorize(
		context.Background(),
		syntax.DID("did:web:alice.example.com"),
		"",
	)
	require.NoError(t, err)
	require.Nil(t, state)
	require.Equal(
		t,
		"https://frontend.example.com/login/habitat?handle=did:web:alice.example.com",
		redirect,
	)
}

func TestLoginProvider_ExchangeRoundTrip(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	token, err := p.issueToken()
	require.NoError(t, err)
	require.NotEmpty(t, token)

	did := syntax.DID("did:web:alice.example.com")

	require.NoError(t, p.Exchange(context.Background(), did, did.String(), token, "", nil))
}

func TestLoginProvider_Exchange_InvalidToken(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	did := syntax.DID("did:web:alice.example.com")
	err := p.Exchange(context.Background(), did, did.String(), "not-a-jwt", "", nil)
	require.ErrorIs(t, err, errInvalidLoginToken)
}

func TestLoginProvider_Exchange_WrongSigningSecret(t *testing.T) {
	p, _ := newTestLoginProvider(t)

	// Token signed with a different secret should fail verification.
	other := []byte("a-different-secret-for-this-test")
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: other}, nil)
	require.NoError(t, err)
	claims := loginTokenClaims{
		Claims: jwt.Claims{Expiry: jwt.NewNumericDate(time.Now().Add(time.Minute))},
	}
	tok, err := jwt.Signed(sig).Claims(claims).CompactSerialize()
	require.NoError(t, err)

	did := syntax.DID("did:web:alice.example.com")

	err = p.Exchange(context.Background(), did, did.String(), tok, "", nil)
	require.ErrorIs(t, err, errInvalidLoginToken)
}

func TestLoginProvider_Exchange_ExpiredToken(t *testing.T) {
	p, _ := newTestLoginProvider(t)

	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.HS256, Key: p.signingSecret}, nil)
	require.NoError(t, err)
	claims := loginTokenClaims{
		Claims: jwt.Claims{Expiry: jwt.NewNumericDate(time.Now().Add(-time.Minute))},
	}
	tok, err := jwt.Signed(sig).Claims(claims).CompactSerialize()
	require.NoError(t, err)

	did := syntax.DID("did:web:alice.example.com")

	err = p.Exchange(context.Background(), did, did.String(), tok, "", nil)
	require.ErrorIs(t, err, errInvalidLoginToken)
}

func mintMember(t *testing.T, s *orgImpl) {
	t.Helper()
	ctx := context.Background()
	token, err := s.IssueIdentityToken(ctx, did1, false, time.Now().Add(time.Hour))
	require.NoError(t, err)
	_, err = s.CreateNewMemberIdentity(ctx, token, "alice", testPassword)
	require.NoError(t, err)
}

func TestLoginProvider_HandlePasswordLogin_Success(t *testing.T) {
	p, s := newTestLoginProvider(t)
	mintMember(t, s)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgLoginMemberInput{
		Handle:   "did:web:alice.example.com",
		Password: testPassword,
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)

	dump, err := httputil.DumpResponse(w.Result(), true)
	t.Log(string(dump))

	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgLoginMemberOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))

	callbackUrl, err := url.Parse(out.CallbackURL)
	require.NoError(t, err, "failed to parse callback URL")

	require.Equal(t, callbackUrl.Scheme, "https")
	require.Equal(t, callbackUrl.Host, "pear.example.com")
	require.Equal(t, callbackUrl.Path, "/oauth-callback")

	code := callbackUrl.Query().Get("code")

	did := syntax.DID("did:web:alice.example.com")
	require.NoError(
		t,
		p.Exchange(context.Background(), did, did.String(), code, "", nil),
	)
}

func TestLoginProvider_HandlePasswordLogin_BadRequestBody(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader([]byte("not-json")))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLoginProvider_HandlePasswordLogin_WrongPassword(t *testing.T) {
	p, s := newTestLoginProvider(t)
	mintMember(t, s)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgLoginMemberInput{
		Handle:   "alice.testorg.example.com",
		Password: "wrong-password",
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestLoginProvider_HandlePasswordLogin_UnknownHandle(t *testing.T) {
	p, _ := newTestLoginProvider(t)

	body, _ := json.Marshal(habitat.NetworkHabitatOrgLoginMemberInput{
		Handle:   "nobody.example.com",
		Password: testPassword,
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
