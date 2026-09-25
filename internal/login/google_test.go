package login

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"

	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
)

// makeIDToken builds a Google-shaped ID token carrying claims, unsigned
// (verifyGoogleIDToken only decodes and validates claims; it doesn't check a
// signature — see its doc comment).
func makeIDToken(t *testing.T, claims googleIDTokenClaims) string {
	t.Helper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	return token
}

func defaultTestClaims(clientID, email string) googleIDTokenClaims {
	now := time.Now()
	return googleIDTokenClaims{
		Email:         email,
		EmailVerified: true,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://accounts.google.com",
			Audience:  jwt.ClaimStrings{clientID},
			Subject:   "123",
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
		},
	}
}

func TestGoogleProvider_Authorize(t *testing.T) {
	p, err := NewGoogleProvider(
		"client-id",
		"client-secret",
		"https://example.com/callback",
		testutil.NewDB(t),
		encrypt.TestKey,
	)
	require.NoError(t, err)

	redirect, state, err := p.Authorize(t.Context(), "user@gmail.com")
	require.NoError(t, err)
	require.Contains(t, redirect, "https://accounts.google.com/o/oauth2/v2/auth")
	require.Contains(t, redirect, "login_hint=user%40gmail.com")
	require.Contains(t, redirect, "code_challenge=")
	require.Contains(t, redirect, "access_type=offline")
	require.NotEmpty(t, state)

	var s googleProviderState
	require.NoError(t, json.Unmarshal(state, &s))
	require.NotEmpty(t, s.Verifier)
	require.NotEmpty(t, s.State)
}

func TestGoogleProvider_Exchange(t *testing.T) {
	clientID := "test-client-id.apps.googleusercontent.com"
	p, err := NewGoogleProvider(
		clientID,
		"test-secret",
		"https://example.com/callback",
		testutil.NewDB(t),
		encrypt.TestKey,
	)
	require.NoError(t, err)

	idToken := makeIDToken(t, defaultTestClaims(clientID, "user@gmail.com"))

	tokenServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			require.Equal(t, "POST", r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, "authorization_code", r.Form.Get("grant_type"))
			require.NotEmpty(t, r.Form.Get("code"))
			require.NotEmpty(t, r.Form.Get("code_verifier"))

			resp := map[string]any{
				"access_token":  "ya29.google-access-token",
				"refresh_token": "1//google-refresh-token",
				"expires_in":    3600,
				"token_type":    "Bearer",
				"id_token":      idToken,
			}
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(resp))
		}),
	)
	defer tokenServer.Close()

	gp := p.(*googleProvider)
	gp.oauthCfg.Endpoint.TokenURL = tokenServer.URL

	_, state, err := p.Authorize(t.Context(), "")
	require.NoError(t, err)

	var gs googleProviderState
	require.NoError(t, json.Unmarshal(state, &gs))

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, tokenServer.Client())
	loginID, err := p.Exchange(ctx, url.Values{"code": {"auth-code"}, "state": {gs.State}}, state)
	require.NoError(t, err)
	require.Equal(t, "user@gmail.com", loginID)

	creds, err := gp.GetCredentials(ctx, "user@gmail.com")
	require.NoError(t, err)
	require.Equal(t, "ya29.google-access-token", creds.AccessToken)
	require.Equal(t, "1//google-refresh-token", creds.RefreshToken)
	require.Equal(t, "user@gmail.com", creds.Email)
	require.Equal(t, idToken, creds.IDToken)
}

func TestVerifyGoogleIDToken(t *testing.T) {
	clientID := "my-client-id.apps.googleusercontent.com"

	t.Run("valid token returns email", func(t *testing.T) {
		token := makeIDToken(t, defaultTestClaims(clientID, "user@gmail.com"))
		claims, err := verifyGoogleIDToken(token, clientID)
		require.NoError(t, err)
		require.Equal(t, "user@gmail.com", claims.Email)
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		token := makeIDToken(t, defaultTestClaims("other-client-id", "user@gmail.com"))
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
	})

	t.Run("unverified email rejected", func(t *testing.T) {
		claims := defaultTestClaims(clientID, "unverified@example.com")
		claims.EmailVerified = false
		token := makeIDToken(t, claims)
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "email not verified")
	})

	t.Run("malformed token rejected", func(t *testing.T) {
		_, err := verifyGoogleIDToken("not.a.jwt", clientID)
		require.Error(t, err)
	})

	t.Run("expired token rejected", func(t *testing.T) {
		claims := defaultTestClaims(clientID, "old@example.com")
		claims.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Hour))
		token := makeIDToken(t, claims)
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
	})

	t.Run("unexpected issuer rejected", func(t *testing.T) {
		claims := defaultTestClaims(clientID, "user@gmail.com")
		claims.Issuer = "https://evil.example.com"
		token := makeIDToken(t, claims)
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "unexpected id token issuer")
	})
}
