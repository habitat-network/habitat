package oauthserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOAuthServerErrorPaths(t *testing.T) {
	t.Run("NewOAuthServer rejects invalid secret", func(t *testing.T) {
		_, err := NewOAuthServer(
			"name", "endpoint", "not-valid-base64!!!",
			nil, nil, nil, nil, nil,
		)
		require.Error(t, err)
	})

	// Common setup for all handler tests.
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	clientMetadata := &pdsclient.ClientMetadata{}
	oauthClient := NewDummyOAuthClient(t, clientMetadata)
	defer oauthClient.Close()
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	oauthSrv, err := NewOAuthServer(
		"testServiceName",
		"testServiceEndpoint",
		secret,
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		pdsclient.NewDummyDirectory("http://pds.url"),
		credStore,
		db,
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthSrv.HandleAuthorize(w, r)
		case "/callback":
			oauthSrv.HandleCallback(w, r)
		case "/token":
			oauthSrv.HandleToken(w, r)
		case "/client-metadata":
			oauthSrv.HandleClientMetadata(w, r)
		case "/resource":
			oauthSrv.Validate(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("CanHandle returns true for oauth header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Habitat-Auth-Method", "oauth")
		require.True(t, oauthSrv.CanHandle(r))
	})

	t.Run("CanHandle returns false without oauth header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		require.False(t, oauthSrv.CanHandle(r))
	})

	t.Run("HandleClientMetadata returns metadata as JSON", func(t *testing.T) {
		resp, err := server.Client().Get(server.URL + "/client-metadata")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusOK, resp.StatusCode)
		require.Equal(t, "application/json", resp.Header.Get("Content-Type"))
	})

	t.Run("HandleAuthorize rejects request missing OAuth params", func(t *testing.T) {
		// No client_id, redirect_uri, etc. — fosite will reject the authorize request.
		resp, err := server.Client().Get(server.URL + "/authorize")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.NotEqual(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("HandleCallback rejects request with no session flash", func(t *testing.T) {
		// No prior /authorize call, so the session contains no flash data.
		resp, err := server.Client().Get(server.URL + "/callback")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("HandleToken rejects invalid token request", func(t *testing.T) {
		resp, err := server.Client().Post(
			server.URL+"/token",
			"application/x-www-form-urlencoded",
			http.NoBody,
		)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.NotEqual(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("Validate rejects request with no token", func(t *testing.T) {
		resp, err := server.Client().Get(server.URL + "/resource")
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("Validate rejects malformed JWT", func(t *testing.T) {
		req, err := http.NewRequest(http.MethodGet, server.URL+"/resource", nil)
		require.NoError(t, err)
		req.Header.Set("Authorization", "Bearer not.a.valid.jwt")
		resp, err := server.Client().Do(req)
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	})
}

// errLookupDIDDirectory wraps DummyDirectory but returns an error from LookupDID,
// simulating a user whose DID document cannot be resolved during the callback.
type errLookupDIDDirectory struct {
	*pdsclient.DummyDirectory
}

func (d *errLookupDIDDirectory) LookupDID(
	_ context.Context,
	_ syntax.DID,
) (*identity.Identity, error) {
	return nil, fmt.Errorf("simulated DID doc lookup failure")
}

// newClientApp returns a test server that serves OAuth client metadata.
// The returned URL is the server's base URL; the metadata's client_id uses that URL.
func newClientApp(t *testing.T, serverCallbackURL string) *httptest.Server {
	t.Helper()
	var clientApp *httptest.Server
	clientApp = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
			ClientId:      clientApp.URL + "/client-metadata.json",
			RedirectUris:  []string{serverCallbackURL},
			ResponseTypes: []string{"code"},
			GrantTypes:    []string{"authorization_code"},
		})
	}))
	t.Cleanup(clientApp.Close)
	return clientApp
}

func TestHandleCallbackDIDDocError(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)

	clientMetadata := &pdsclient.ClientMetadata{}
	oauthClient := NewDummyOAuthClient(t, clientMetadata)
	defer oauthClient.Close()

	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)

	oauthSrv, err := NewOAuthServer(
		"testServiceName", "testServiceEndpoint", secret,
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		&errLookupDIDDirectory{pdsclient.NewDummyDirectory("http://pds.url")},
		credStore,
		db,
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthSrv.HandleAuthorize(w, r)
		case "/callback":
			oauthSrv.HandleCallback(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	httpClient := server.Client()
	httpClient.Jar = jar
	clientMetadata.RedirectUris = []string{server.URL + "/callback"}

	clientApp := newClientApp(t, server.URL+"/callback")
	verifier := oauth2.GenerateVerifier()
	config := &oauth2.Config{
		ClientID:    clientApp.URL + "/client-metadata.json",
		RedirectURL: server.URL + "/callback",
		Endpoint:    oauth2.Endpoint{AuthURL: server.URL + "/authorize"},
	}

	req, err := http.NewRequest(
		http.MethodGet,
		config.AuthCodeURL(
			"test-state",
			oauth2.S256ChallengeOption(verifier),
		)+"&handle=did:web:test",
		nil,
	)
	require.NoError(t, err)

	resp, err := httpClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestOAuthServerE2E(t *testing.T) {
	// setup test database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "failed to open test database")

	// setup pds credential store
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err, "failed to setup pds credential store")

	// setup oauth server
	clientMetadata := &pdsclient.ClientMetadata{}
	oauthClient := NewDummyOAuthClient(t, clientMetadata)
	defer oauthClient.Close()

	// Generate RSA key for JWT signing
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err, "failed to generate secret")

	oauthServer, err := NewOAuthServer(
		"testServiceName",
		"testServiceEndpoint",
		secret,
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		pdsclient.NewDummyDirectory("http://pds.url"),
		credStore,
		db,
	)
	require.NoError(t, err, "failed to setup oauth server")

	// setup http server oauth client to make requests to
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
			return
		case "/callback":
			oauthServer.HandleCallback(w, r)
			return
		case "/token":
			oauthServer.HandleToken(w, r)
			return
		case "/resource":
			did, ok := oauthServer.Validate(w, r)
			require.True(t, ok, "failed to validate token")
			require.Equal(t, syntax.DID("did:web:test"), did)
		default:
			t.Errorf("unknown server path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	require.NoError(t, err, "failed to create cookie jar")
	server.Client().Jar = jar
	// set the server's oauthClient redirectUri now that we know the url
	clientMetadata.RedirectUris = []string{server.URL + "/callback"}

	// setup client app that oauth server can make requests to
	verifier := oauth2.GenerateVerifier()
	config := &oauth2.Config{
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/authorize",
			TokenURL: server.URL + "/token",
		},
	}
	clientApp := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/client-metadata.json":
				w.Header().Set("Content-Type", "application/json")
				err := json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
					ClientId:      "http://" + r.Host + "/client-metadata.json",
					RedirectUris:  []string{"http://" + r.Host + "/callback"},
					ResponseTypes: []string{"code"},
					GrantTypes:    []string{"authorization_code", "refresh_token"},
				})
				require.NoError(t, err, "failed to encode client metadata")
				return
			case "/callback":
				ctx := context.WithValue(r.Context(), oauth2.HTTPClient, server.Client())
				token, err := config.Exchange(
					ctx,
					r.URL.Query().Get("code"),
					oauth2.VerifierOption(verifier),
				)
				require.NoError(t, err, "failed to exchange token")
				require.NoError(t, json.NewEncoder(w).Encode(token))
				return
			default:
				t.Logf("unknown client app path: %v", r.URL.Path)
				t.Fail()
			}
		}),
	)
	defer clientApp.Close()

	// Set the client app's OAuth configuration now that we know the url
	config.ClientID = clientApp.URL + "/client-metadata.json"
	config.RedirectURL = clientApp.URL + "/callback"

	// create authorize request
	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:test", nil)
	require.NoError(t, err, "failed to create authorize request")

	// make authorize requests which will follow redirects all thw way to token response
	result, err := server.Client().Do(authRequest)
	require.NoError(t, err, "failed to make authorize request")
	respBytes, err := io.ReadAll(result.Body)
	require.NoError(t, err, "failed to read response body")
	require.NoError(t, result.Body.Close())
	require.Equal(
		t,
		http.StatusOK,
		result.StatusCode,
		"authorize request failed: %s",
		respBytes,
	)

	token := &oauth2.Token{}
	require.NoError(t, json.Unmarshal(respBytes, token), "failed to decode token")
	require.NotEmpty(t, token.AccessToken, "access token should not be empty")
	require.NotEmpty(t, token.RefreshToken, "refresh token should not be empty")

	// use server as the oauth client http client because of it has the tls cert
	oauthClientCtx := context.WithValue(
		context.Background(),
		oauth2.HTTPClient,
		server.Client(),
	)
	client := config.Client(oauthClientCtx, token)

	resp, err := client.Get(server.URL + "/resource")
	require.NoError(t, err, "failed to make resource request")
	respBytes, err = io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "resource request failed: %s", respBytes)

	// force expire the token to test refresh
	token.Expiry = time.Now().Add(-time.Hour)
	client = config.Client(oauthClientCtx, token)

	// retry to test refresh
	resp, err = client.Get(server.URL + "/resource")
	require.NoError(t, err, "failed to make resource request")
	respBytes, err = io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "resource request failed: %s", respBytes)
}
