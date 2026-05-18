package login

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/googlecred"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// stubOAuthClient is a minimal PdsOAuthClient for testing.
type stubOAuthClient struct {
	authorizeErr    error
	exchangeCodeErr error
	redirectURL     string
}

func (s *stubOAuthClient) ClientMetadata() *pdsclient.ClientMetadata { return nil }

func (s *stubOAuthClient) Authorize(
	_ *pdsclient.DpopHttpClient,
	_ *identity.Identity,
) (string, *pdsclient.AuthorizeState, error) {
	if s.authorizeErr != nil {
		return "", nil, s.authorizeErr
	}
	return s.redirectURL, &pdsclient.AuthorizeState{
		Verifier:      "verifier",
		State:         "state",
		TokenEndpoint: "https://pds.example.com/token",
	}, nil
}

func (s *stubOAuthClient) ExchangeCode(
	_ *pdsclient.DpopHttpClient,
	_, _ string,
	_ *pdsclient.AuthorizeState,
) (*pdsclient.TokenResponse, error) {
	if s.exchangeCodeErr != nil {
		return nil, s.exchangeCodeErr
	}
	return &pdsclient.TokenResponse{
		AccessToken:  "access",
		RefreshToken: "refresh",
	}, nil
}

func (s *stubOAuthClient) RefreshToken(
	_ *pdsclient.DpopHttpClient,
	_ *identity.Identity,
	_ string,
	_ string,
) (*pdsclient.TokenResponse, error) {
	return nil, errors.New("not used in these tests")
}

// stubCredStore tracks UpsertCredentials calls.
type stubCredStore struct {
	upserted  map[syntax.DID]*pdscred.Credentials
	upsertErr error
}

func newStubCredStore() *stubCredStore {
	return &stubCredStore{upserted: make(map[syntax.DID]*pdscred.Credentials)}
}

func (s *stubCredStore) UpsertCredentials(
	_ context.Context,
	did syntax.DID,
	creds *pdscred.Credentials,
) error {
	if s.upsertErr != nil {
		return s.upsertErr
	}
	s.upserted[did] = creds
	return nil
}

func (s *stubCredStore) GetCredentials(
	_ context.Context,
	did syntax.DID,
) (*pdscred.Credentials, error) {
	return nil, errors.New("not used in these tests")
}

// helpers to build identities with specific service combinations

func idWithPDSOnly() *identity.Identity {
	return &identity.Identity{
		DID: "did:web:pds.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {URL: "https://pds.example.com"},
		},
	}
}

// --- pdsProvider ---

func TestPDSProvider_Authorize(t *testing.T) {
	client := &stubOAuthClient{redirectURL: "https://pds.example.com/authorize"}
	p := NewPDSProvider(client, newStubCredStore(), nil)

	redirect, state, err := p.Authorize(context.Background(), idWithPDSOnly(), "")
	require.NoError(t, err)
	require.Equal(t, "https://pds.example.com/authorize", redirect)
	require.NotEmpty(t, state)

	// state must round-trip through Exchange — verify it's valid JSON with expected fields
	var s pdsProviderState
	require.NoError(t, unmarshalProviderState(state, &s))
	require.NotEmpty(t, s.DpopKey)
	require.Equal(t, "verifier", s.AuthorizeState.Verifier)
}

func TestPDSProvider_Exchange(t *testing.T) {
	credStore := newStubCredStore()
	p := NewPDSProvider(
		&stubOAuthClient{redirectURL: "https://pds.example.com/authorize"},
		credStore,
		nil,
	)
	did := syntax.DID("did:web:pds.example.com")

	// Obtain valid state from Authorize.
	_, state, err := p.Authorize(context.Background(), idWithPDSOnly(), "")
	require.NoError(t, err)

	err = p.Exchange(context.Background(), did, "code", "https://pds.example.com", state)
	require.NoError(t, err)

	creds, stored := credStore.upserted[did]
	require.True(t, stored, "credentials should have been upserted")
	require.Equal(t, "access", creds.AccessToken)
	require.Equal(t, "refresh", creds.RefreshToken)
	require.NotNil(t, creds.DpopKey)
}

// dummyProvider is a test stand-in for any non-PDS provider.
type dummyProvider struct{}

func NewDummyProvider() Provider { return &dummyProvider{} }

func (d *dummyProvider) LoginMethod() org.LoginMethod { return org.LoginMethodPassword }

func (d *dummyProvider) Authorize(
	_ context.Context,
	_ *identity.Identity,
	_ string,
) (string, []byte, error) {
	return "https://dummy.example.com/login", nil, nil
}
func (d *dummyProvider) Exchange(_ context.Context, _ syntax.DID, _, _ string, _ []byte) error {
	return nil
}

// --- Router ---

func newTestRouter() *Router {
	return NewRouter(
		NewPDSProvider(&stubOAuthClient{}, newStubCredStore(), nil),
		NewDummyProvider(),
	)
}

func TestRouter_ByLoginMethod(t *testing.T) {
	r := newTestRouter()

	p, err := r.ByLoginMethod(org.LoginMethodAtproto)
	require.NoError(t, err)
	require.Equal(t, org.LoginMethodAtproto, p.LoginMethod())

	p, err = r.ByLoginMethod(org.LoginMethodPassword)
	require.NoError(t, err)
	require.Equal(t, org.LoginMethodPassword, p.LoginMethod())

	_, err = r.ByLoginMethod("unknown")
	require.Error(t, err)
}

func TestPDSProvider_AuthorizeWithLoginID(t *testing.T) {
	client := &stubOAuthClient{redirectURL: "https://public-pds.example.com/authorize"}
	dir := pdsclient.NewDummyDirectory("https://public-pds.example.com")
	p := NewPDSProvider(client, newStubCredStore(), dir)

	orgID := &identity.Identity{
		DID: "did:web:internal.org.example.com",
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://habitat.example.com"},
		},
	}
	redirect, state, err := p.Authorize(context.Background(), orgID, "did:plc:mapped-public")
	require.NoError(t, err)
	require.Equal(t, "https://public-pds.example.com/authorize", redirect)
	require.NotEmpty(t, state)
}

// --- googleProvider ---

func newGoogleCredStore(t *testing.T) googlecred.GoogleCredentialStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	s, err := googlecred.NewGoogleCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	return s
}

func makeIDToken(clientID, email string) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload := base64.RawURLEncoding.EncodeToString([]byte(
		fmt.Sprintf(
			`{"iss":"https://accounts.google.com","aud":"%s","sub":"123","email":"%s","email_verified":true,"iat":1000000000,"exp":9999999999}`,
			clientID,
			email,
		),
	))
	return header + "." + payload + ".fakesignature"
}

func TestGoogleProvider_LoginMethod(t *testing.T) {
	p := NewGoogleProvider(
		"client-id",
		"client-secret",
		"https://example.com/callback",
		newGoogleCredStore(t),
	)
	require.Equal(t, "google", p.LoginMethod())
}

func TestGoogleProvider_Authorize(t *testing.T) {
	p := NewGoogleProvider(
		"client-id",
		"client-secret",
		"https://example.com/callback",
		newGoogleCredStore(t),
	)

	redirect, state, err := p.Authorize(context.Background(), idWithPDSOnly(), "user@gmail.com")
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

func TestGoogleProvider_Authorize_NoLoginID(t *testing.T) {
	p := NewGoogleProvider(
		"client-id",
		"client-secret",
		"https://example.com/callback",
		newGoogleCredStore(t),
	)

	_, _, err := p.Authorize(context.Background(), idWithPDSOnly(), "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no google email configured")
}

func TestGoogleProvider_Exchange(t *testing.T) {
	credStore := newGoogleCredStore(t)
	clientID := "test-client-id.apps.googleusercontent.com"
	p := NewGoogleProvider(clientID, "test-secret", "https://example.com/callback", credStore)

	idToken := makeIDToken(clientID, "user@gmail.com")

	// Mock Google's token endpoint
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

	// Override the token endpoint to point at our mock
	gp := p.(*googleProvider)
	gp.oauthCfg.Endpoint.TokenURL = tokenServer.URL

	// Drive Authorize to get valid state
	_, state, err := p.Authorize(context.Background(), idWithPDSOnly(), "user@gmail.com")
	require.NoError(t, err)

	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, tokenServer.Client())
	did := syntax.DID("did:web:example.com")
	err = p.Exchange(ctx, did, "auth-code", "", state)
	require.NoError(t, err)

	creds, err := credStore.GetCredentials(ctx, did)
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

// unmarshalProviderState is a test helper to inspect the opaque pds state bytes.
func unmarshalProviderState(b []byte, s *pdsProviderState) error {
	return json.Unmarshal(b, s)
}
