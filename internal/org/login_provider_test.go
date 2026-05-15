package org

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newTestLoginProvider(t *testing.T) (*LoginProvider, *orgImpl) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)

	s, err := NewStore(db, h, identity.DefaultDirectory(), "pear.example.com")
	require.NoError(t, err)

	orgId, _, err := s.CreateOrg(t.Context(), "test-org", "admin", "password")
	require.NoError(t, err)

	scoped, err := s.GetOrg(context.Background(), orgId)
	require.NoError(t, err)

	return NewLoginProvider(
		s,
		"pear.example.com",
		"frontend.example.com",
		testSigningSecret,
	), scoped.(*orgImpl)
}

func TestLoginProvider_Type(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	require.Equal(t, "habitat", p.Type())
}

func TestLoginProvider_CanHandle(t *testing.T) {
	p, _ := newTestLoginProvider(t)

	habitatOnly := &identity.Identity{
		DID: "did:web:habitat.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://habitat.example.com"},
		},
	}
	pdsOnly := &identity.Identity{
		DID: "did:web:pds.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: "https://pds.example.com"},
		},
	}
	both := &identity.Identity{
		DID: "did:web:both.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: "https://pds.example.com"},
			"habitat":     {URL: "https://habitat.example.com"},
		},
	}
	none := &identity.Identity{
		DID:      "did:web:nobody.example.com",
		Services: map[string]identity.ServiceEndpoint{},
	}

	require.True(t, p.CanHandle(habitatOnly))
	require.False(t, p.CanHandle(pdsOnly))
	require.False(t, p.CanHandle(both))
	require.False(t, p.CanHandle(none))
}

func TestLoginProvider_Authorize(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	id := &identity.Identity{
		Handle: "alice.example.com",
	}
	redirect, state, err := p.Authorize(context.Background(), id)
	require.NoError(t, err)
	require.Nil(t, state)
	require.Equal(
		t,
		"https://frontend.example.com/login/habitat?handle=alice.example.com",
		redirect,
	)
}

func TestLoginProvider_Authorize_HandleEscaping(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	id := &identity.Identity{
		Handle: "alice bob",
	}
	redirect, _, err := p.Authorize(context.Background(), id)
	require.NoError(t, err)
	require.Contains(t, redirect, "handle=alice+bob")
}

func TestLoginProvider_ExchangeRoundTrip(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	token, err := p.issueToken()
	require.NoError(t, err)
	require.NotEmpty(t, token)

	require.NoError(t, p.Exchange(context.Background(), syntax.DID(""), token, "", nil))
}

func TestLoginProvider_Exchange_InvalidToken(t *testing.T) {
	p, _ := newTestLoginProvider(t)
	err := p.Exchange(context.Background(), syntax.DID(""), "not-a-jwt", "", nil)
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

	err = p.Exchange(context.Background(), syntax.DID(""), tok, "", nil)
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

	err = p.Exchange(context.Background(), syntax.DID(""), tok, "", nil)
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
		Handle:   "alice.example.com",
		Password: testPassword,
	})
	req := httptest.NewRequest(http.MethodPost, "/login", bytes.NewReader(body))
	w := httptest.NewRecorder()
	p.HandlePasswordLogin(w, req)

	require.Equal(t, http.StatusOK, w.Code)

	var out habitat.NetworkHabitatOrgLoginMemberOutput
	require.NoError(t, json.NewDecoder(w.Body).Decode(&out))
	require.Contains(t, out.CallbackURL, "/oauth-callback?code=")

	code := out.CallbackURL[len("/oauth-callback?code="):]
	require.NoError(t, p.Exchange(context.Background(), syntax.DID(""), code, "", nil))
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
		Handle:   "alice.example.com",
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
