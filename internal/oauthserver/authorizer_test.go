package oauthserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/ory/fosite"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
)

func TestAuthorizerCodeAndRefresh(t *testing.T) {
	ctx := t.Context()
	kv := keyval_testutil.New(t)
	server, err := NewOAuthServer(
		encrypt.TestKey, nil, nil, dbtestutil.NewDB(t, Models()), kv, noop.Meter{},
		"https://habitat.example", NewJWTBearerStore(), testOpensocialStore(t), nil, "",
	)
	require.NoError(t, err)

	const group = "did:web:group.example"
	const actor = syntax.DID("did:web:member.example")
	const redirect = "http://127.0.0.1:5173/callback"
	clientID := "http://localhost?scope=atproto&redirect_uri=" + url.QueryEscape(
		"http://127.0.0.1/callback",
	)
	verifier := oauth2.GenerateVerifier()
	authReq := httptest.NewRequest(http.MethodGet, "/authorize?"+url.Values{
		"client_id": {clientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"scope": {"atproto"}, "state": {"test-state"}, "code_challenge_method": {"S256"},
		"code_challenge": {oauth2.S256ChallengeFromVerifier(verifier)},
	}.Encode(), http.NoBody).WithContext(ctx)
	requester, err := server.provider.NewAuthorizeRequest(ctx, authReq)
	require.NoError(t, err)
	requester.GrantScope("atproto")

	// Test only the post-consent boundary. No production authorization handler
	// creates a session with an authorizer yet.
	verified := &session{
		Subject:    group,
		Authorizer: actor,
		ClientID:   clientID,
		Scopes:     []string{"atproto"},
	}
	requester.SetSession(verified)
	require.NoError(t, server.storage.CreatePARSession(ctx, "urn:req:test", requester))
	pending, err := server.storage.GetPARSession(ctx, "urn:req:test")
	require.NoError(t, err)
	require.Empty(t, pending.GetSession().(*session).Authorizer)
	require.Equal(t, actor, verified.Clone().(*session).Authorizer)
	response, err := server.provider.NewAuthorizeResponse(ctx, requester, verified)
	require.NoError(t, err)
	code := response.GetCode()
	require.NotEmpty(t, code)

	token := func(values url.Values) oauth2.Token {
		t.Helper()
		values.Set("client_id", clientID)
		req := httptest.NewRequest(http.MethodPost, "/token", strings.NewReader(values.Encode())).
			WithContext(ctx)
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		server.HandleToken(rec, req)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		var got oauth2.Token
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
		require.NotEmpty(t, got.RefreshToken)
		cred, ok, err := server.ValidateRaw(ctx, got.AccessToken, "atproto")
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, syntax.DID(group), cred.Subject)
		require.Equal(t, actor, cred.Authorizer)
		require.Equal(t, []string{"atproto"}, cred.Scopes)
		_, _, err = server.ValidateRaw(ctx, got.AccessToken, "transition:generic")
		require.Error(t, err)
		return got
	}

	first := token(url.Values{
		"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier},
	})
	second := token(
		url.Values{"grant_type": {"refresh_token"}, "refresh_token": {first.RefreshToken}},
	)
	require.NotEqual(t, first.RefreshToken, second.RefreshToken)
	third := token(
		url.Values{"grant_type": {"refresh_token"}, "refresh_token": {second.RefreshToken}},
	)
	require.NotEqual(t, second.RefreshToken, third.RefreshToken)
	_, err = server.storage.GetRefreshTokenSession(ctx, tokenSignature(first.RefreshToken), nil)
	require.ErrorIs(t, err, fosite.ErrNotFound)
}

func tokenSignature(token string) string {
	_, sig, _ := strings.Cut(token, ".")
	return sig
}
