package pdsoauth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	"github.com/stretchr/testify/require"
)

func TestAuthOnlyPDS(t *testing.T) {
	const issuer = "https://pds.example.com"
	const otherDID syntax.DID = "did:web:someone-else.example"

	writer, kv := newTestStore(t)
	ctx := t.Context()
	writerSession := oauth.ClientSessionData{
		AccountDID: testDID, SessionID: "writer",
		AccessToken: "writer-token", Scopes: []string{"atproto", "transition:generic"},
	}
	require.NoError(t, writer.SaveSession(ctx, writerSession))
	require.NoError(
		t,
		writer.SaveAuthRequestInfo(ctx, oauth.AuthRequestData{State: "writer-state"}),
	)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	cfg := Config{
		ClientID:    "https://pear.example.com/oauth-client",
		CallbackURL: "https://pear.example.com/oauth-callback",
		Secret:      secret,
	}
	dir := NewDummyDirectory(issuer)
	auth, err := NewAuthOnly(cfg, kv, dir)
	require.NoError(t, err)
	writeApp, err := NewApp(cfg, writer, dir)
	require.NoError(t, err)
	require.Equal(t, "atproto transition:generic", writeApp.ClientMetadata().Scope)

	var state, challenge string
	target := testDID
	grant := "atproto"
	exchanges := 0
	client := &http.Client{
		Transport: testPDSRoundTrip(func(r *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			var body any
			switch r.URL.Path {
			case "/.well-known/oauth-protected-resource":
				body = oauth.ProtectedResourceMetadata{AuthorizationServers: []string{issuer}}
			case "/.well-known/oauth-authorization-server":
				body = oauth.AuthServerMetadata{
					Issuer:                             issuer,
					AuthorizationEndpoint:              issuer + "/authorize",
					TokenEndpoint:                      issuer + "/token",
					PushedAuthorizationRequestEndpoint: issuer + "/par",
					ResponseTypesSupported:             []string{"code"},
					GrantTypesSupported: []string{
						"authorization_code",
						"refresh_token",
					},
					ScopesSupported: []string{
						"atproto",
						"transition:generic",
					},
					CodeChallengeMethodsSupported:              []string{"S256"},
					TokenEndpointAuthMethodsSupoorted:          []string{"none", "private_key_jwt"},
					TokenEndpointAuthSigningAlgValuesSupported: []string{"ES256"},
					DPoPSigningAlgValuesSupported:              []string{"ES256"},
					AuthorizationReponseISSParameterSupported:  true,
					RequirePushedAuthorizationRequests:         true,
					ClientIDMetadataDocumentSupported:          true,
				}
			case "/par":
				require.NoError(t, r.ParseForm())
				require.Equal(t, "atproto", r.PostForm.Get("scope"))
				require.Equal(t, testDID.String(), r.PostForm.Get("login_hint"))
				state, challenge = r.PostForm.Get("state"), r.PostForm.Get("code_challenge")
				require.NotEmpty(t, state)
				require.NotEmpty(t, challenge)
				body = oauth.PushedAuthResponse{RequestURI: "urn:request:example", ExpiresIn: 60}
			case "/token":
				exchanges++
				require.NoError(t, r.ParseForm())
				require.Equal(
					t,
					challenge,
					oauth.S256CodeChallenge(r.PostForm.Get("code_verifier")),
				)
				body = oauth.TokenResponse{
					Subject:      target.String(),
					Scope:        grant,
					AccessToken:  "auth-token",
					RefreshToken: "auth-refresh",
				}
			default:
				t.Fatalf("unexpected request: %s", r.URL.Path)
			}
			require.NoError(t, json.NewEncoder(rec).Encode(body))
			return rec.Result(), nil
		}),
	}
	auth.app.Client, auth.app.Resolver.Client = client, client

	start := func(t *testing.T) url.Values {
		t.Helper()
		redirect, pending, err := auth.Start(ctx, testDID)
		require.NoError(t, err)
		require.Equal(t, state, pending)
		require.Contains(t, redirect, issuer+"/authorize?")
		return url.Values{"state": {pending}, "iss": {issuer}, "code": {"approved"}}
	}

	params := start(t)
	_, err = writer.GetAuthRequestInfo(ctx, state)
	require.ErrorIs(t, err, keyval.ErrNotFound)
	_, err = auth.Verify(ctx, params, "another-request", testDID)
	require.ErrorContains(t, err, "does not match the pending authentication")
	_, err = auth.Verify(ctx, url.Values{"state": {"writer-state"}}, "writer-state", testDID)
	require.ErrorIs(t, err, keyval.ErrNotFound)
	require.Zero(t, exchanges)
	actor, err := auth.Verify(ctx, params, state, testDID)
	require.NoError(t, err)
	require.Equal(t, testDID, actor)
	_, err = auth.Verify(ctx, params, state, testDID)
	require.ErrorIs(t, err, keyval.ErrNotFound)
	require.Equal(t, 1, exchanges)

	for _, tt := range []struct {
		name, scope, want string
		issued, expected  syntax.DID
	}{
		{"extra scope", "atproto transition:generic", "authentication-only scope", testDID, testDID},
		{"missing scope", "", "authentication-only scope", testDID, testDID},
		{"different token subject", "atproto", "token subject didn't match", otherDID, testDID},
		{"different outer actor", "atproto", "different personal account", testDID, otherDID},
	} {
		t.Run(tt.name, func(t *testing.T) {
			target, grant = tt.issued, tt.scope
			params := start(t)
			actor, err := auth.Verify(ctx, params, state, tt.expected)
			require.ErrorContains(t, err, tt.want)
			require.Empty(t, actor)
		})
	}

	got, err := writer.GetSession(ctx, testDID, "")
	require.NoError(t, err)
	require.Equal(t, writerSession, *got)
	_, err = writer.GetAuthRequestInfo(ctx, "writer-state")
	require.NoError(t, err)
}

type testPDSRoundTrip func(*http.Request) (*http.Response, error)

func (fn testPDSRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	return fn(r)
}
