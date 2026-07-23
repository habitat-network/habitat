package login

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	habitatdb "github.com/habitat-network/habitat/internal/db"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func makeIDToken(clientID, email string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString(
		fmt.Appendf(
			nil,
			`{"iss":"https://accounts.google.com","aud":"%s","sub":"123","email":"%s","email_verified":true,"iat":1000000000,"exp":9999999999}`,
			clientID,
			email,
		))
	return header + "." + payload + ".fakesignature"
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
	db := testutil.NewDB(t)
	p, err := NewGoogleProvider(
		clientID,
		"test-secret",
		"https://example.com/callback",
		db,
		encrypt.TestKey,
	)
	require.NoError(t, err)
	require.NoError(t, habitatdb.AutoMigrate(db, p))

	idToken := makeIDToken(clientID, "user@gmail.com")

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

	gp := p
	gp.oauthCfg.Endpoint.TokenURL = tokenServer.URL

	_, state, err := p.Authorize(t.Context(), "")
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, tokenServer.Client())
	loginID, err := p.Exchange(ctx, "auth-code", "", state)
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
		token := makeIDToken(clientID, "user@gmail.com")
		email, err := verifyGoogleIDToken(token, clientID)
		require.NoError(t, err)
		require.Equal(t, "user@gmail.com", email)
	})

	t.Run("wrong audience rejected", func(t *testing.T) {
		token := makeIDToken("other-client-id", "user@gmail.com")
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "audience mismatch")
	})

	t.Run("unverified email rejected", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(
			`{"iss":"https://accounts.google.com","aud":"my-client-id.apps.googleusercontent.com","sub":"123","email":"unverified@example.com","email_verified":false,"iat":1000000000,"exp":9999999999}`,
		))
		token := header + "." + payload + ".fakesig"
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "email not verified")
	})

	t.Run("malformed token rejected", func(t *testing.T) {
		_, err := verifyGoogleIDToken("not.a.jwt", clientID)
		require.Error(t, err)
	})

	t.Run("expired token rejected", func(t *testing.T) {
		header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
		payload := base64.RawURLEncoding.EncodeToString([]byte(
			`{"iss":"https://accounts.google.com","aud":"my-client-id.apps.googleusercontent.com","sub":"123","email":"old@example.com","email_verified":true,"iat":1000000000,"exp":1000000001}`,
		))
		token := header + "." + payload + ".fakesig"
		_, err := verifyGoogleIDToken(token, clientID)
		require.Error(t, err)
		require.Contains(t, err.Error(), "id token expired")
	})
}
