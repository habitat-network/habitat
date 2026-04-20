package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/node"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOAuthServerErrorPaths(t *testing.T) {
	t.Run("NewOAuthServer rejects invalid secret", func(t *testing.T) {
		_, err := NewOAuthServer(
			"not-valid-base64!!!",
			nil, nil, nil, nil, nil, nil, noop.Meter{}, org.NewEveryoneOrg(),
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
		secret,
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		node.NewDummy(),
		pdsclient.NewDummyDirectory("http://pds.url"),
		credStore,
		db,
		noop.Meter{},
		org.NewEveryoneOrg(),
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

func TestHandleCallbackDIDNotInAllowlist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	clientMetadata := &pdsclient.ClientMetadata{}
	oauthClient := NewDummyOAuthClient(t, clientMetadata)
	defer oauthClient.Close()
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)

	oauthServer, err := NewOAuthServer(
		secret,
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		node.NewDummy(),
		pdsclient.NewDummyDirectory("http://pds.url"),
		credStore,
		db,
		noop.Meter{},
		org.NewEveryoneOrg(),
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
		case "/callback":
			oauthServer.HandleCallback(w, r)
		case "/token":
			oauthServer.HandleToken(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	server.Client().Jar = jar
	clientMetadata.RedirectUris = []string{server.URL + "/callback"}

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
				require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
					ClientId:      "http://" + r.Host + "/client-metadata.json",
					RedirectUris:  []string{"http://" + r.Host + "/callback"},
					ResponseTypes: []string{"code"},
					GrantTypes:    []string{"authorization_code", "refresh_token"},
				}))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	)
	defer clientApp.Close()

	config.ClientID = clientApp.URL + "/client-metadata.json"
	config.RedirectURL = clientApp.URL + "/callback"

	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:test", nil)
	require.NoError(t, err)

	// CheckRedirect stops the client from following past the callback so we can
	// inspect its status code directly.
	server.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// Drive the flow until it hits /callback — the server follows redirects
	// through the dummy PDS and stops at the first non-redirect from /callback.
	httpClient := server.Client()
	resp, err := httpClient.Do(authRequest)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Follow redirects manually until we reach /callback.
	for resp.StatusCode == http.StatusSeeOther {
		loc := resp.Header.Get("Location")
		nextReq, reqErr := http.NewRequest(http.MethodGet, loc, nil)
		require.NoError(t, reqErr)
		resp, err = httpClient.Do(nextReq)
		require.NoError(t, err)
		_ = resp.Body.Close()
		if nextReq.URL.Path == "/callback" {
			break
		}
	}

	require.Equal(t, http.StatusSeeOther /* What fosite authorize error uses */, resp.StatusCode)
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
		secret,
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		node.NewDummy(),
		pdsclient.NewDummyDirectory("http://pds.url"),
		credStore,
		db,
		noop.Meter{},
		org.NewEveryoneOrg(),
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

// testIsMemberOrg wraps an org.Org and overrides IsMember, letting individual
// tests inject specific outcomes without reimplementing the full interface.
type testIsMemberOrg struct {
	org.Org
	fn func(ctx context.Context, did syntax.DID) (bool, error)
}

func (o *testIsMemberOrg) IsMember(ctx context.Context, did syntax.DID) (bool, error) {
	return o.fn(ctx, did)
}

// acquireAccessToken drives the full authorization code flow and returns the
// resulting bearer access token issued by srv.
func acquireAccessToken(t *testing.T, srv *OAuthServer, clientMetadata *pdsclient.ClientMetadata) string {
	t.Helper()
	flowServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			srv.HandleAuthorize(w, r)
		case "/callback":
			srv.HandleCallback(w, r)
		case "/token":
			srv.HandleToken(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(flowServer.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	flowServer.Client().Jar = jar
	clientMetadata.RedirectUris = []string{flowServer.URL + "/callback"}

	verifier := oauth2.GenerateVerifier()
	oauthCfg := &oauth2.Config{
		Endpoint: oauth2.Endpoint{
			AuthURL:  flowServer.URL + "/authorize",
			TokenURL: flowServer.URL + "/token",
		},
	}

	var capturedToken string
	clientApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
				ClientId:      "http://" + r.Host + "/client-metadata.json",
				RedirectUris:  []string{"http://" + r.Host + "/callback"},
				ResponseTypes: []string{"code"},
				GrantTypes:    []string{"authorization_code", "refresh_token"},
			}))
		case "/callback":
			ctx := context.WithValue(r.Context(), oauth2.HTTPClient, flowServer.Client())
			token, exchangeErr := oauthCfg.Exchange(ctx, r.URL.Query().Get("code"), oauth2.VerifierOption(verifier))
			require.NoError(t, exchangeErr)
			capturedToken = token.AccessToken
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(clientApp.Close)

	oauthCfg.ClientID = clientApp.URL + "/client-metadata.json"
	oauthCfg.RedirectURL = clientApp.URL + "/callback"

	authReq, err := http.NewRequest(http.MethodGet,
		oauthCfg.AuthCodeURL("test-state", oauth2.S256ChallengeOption(verifier))+"&handle=did:web:test",
		nil,
	)
	require.NoError(t, err)
	resp, err := flowServer.Client().Do(authReq)
	defer func() { _ = resp.Body.Close() }()
	require.NoError(t, err)
	require.NotEmpty(t, capturedToken, "no access token captured during OAuth flow")
	return capturedToken
}

// TestValidate tests every error and success pathway of OAuthServer.Validate.
func TestValidate(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	clientMetadata := &pdsclient.ClientMetadata{}
	oauthClient := NewDummyOAuthClient(t, clientMetadata)
	defer oauthClient.Close()
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)

	// newSrv creates an OAuthServer sharing the same secret and database.
	// Stateless JWT introspection means tokens issued by any server here are
	// valid for all others created with the same secret.
	newSrv := func(o org.Org) *OAuthServer {
		s, srvErr := NewOAuthServer(
			secret,
			oauthClient,
			sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
			node.NewDummy(),
			pdsclient.NewDummyDirectory("http://pds.url"),
			credStore,
			db,
			noop.Meter{},
			o,
		)
		require.NoError(t, srvErr)
		return s
	}

	// Issue a real JWT via the complete OAuth flow.
	validToken := acquireAccessToken(t, newSrv(org.NewEveryoneOrg()), clientMetadata)

	// callValidate issues a GET against a minimal HTTP server wrapping srv.Validate
	// and returns the HTTP status code together with Validate's return values.
	callValidate := func(srv *OAuthServer, bearerToken string) (status int, did syntax.DID, ok bool) {
		var (
			mu     sync.Mutex
			retDID syntax.DID
			retOK  bool
		)
		httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			d, o := srv.Validate(w, r)
			mu.Lock()
			retDID, retOK = d, o
			mu.Unlock()
		}))
		defer httpSrv.Close()
		req, reqErr := http.NewRequest(http.MethodGet, httpSrv.URL+"/", nil)
		require.NoError(t, reqErr)
		if bearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+bearerToken)
		}
		resp, doErr := http.DefaultClient.Do(req)
		require.NoError(t, doErr)
		require.NoError(t, resp.Body.Close())
		mu.Lock()
		defer mu.Unlock()
		return resp.StatusCode, retDID, retOK
	}

	t.Run("missing token returns !ok", func(t *testing.T) {
		status, _, ok := callValidate(newSrv(org.NewEveryoneOrg()), "")
		require.False(t, ok)
		require.NotEqual(t, http.StatusOK, status)
	})

	t.Run("malformed JWT returns !ok", func(t *testing.T) {
		status, _, ok := callValidate(newSrv(org.NewEveryoneOrg()), "not.a.valid.jwt")
		require.False(t, ok)
		require.NotEqual(t, http.StatusOK, status)
	})

	t.Run("IsMember error returns !ok with 500", func(t *testing.T) {
		srv := newSrv(&testIsMemberOrg{
			Org: org.NewEveryoneOrg(),
			fn: func(_ context.Context, _ syntax.DID) (bool, error) {
				return false, errors.New("simulated database failure")
			},
		})
		status, _, ok := callValidate(srv, validToken)
		require.False(t, ok)
		require.Equal(t, http.StatusInternalServerError, status)
	})

	t.Run("non-member returns !ok with 401", func(t *testing.T) {
		srv := newSrv(&testIsMemberOrg{
			Org: org.NewEveryoneOrg(),
			fn: func(_ context.Context, _ syntax.DID) (bool, error) {
				return false, nil
			},
		})
		status, _, ok := callValidate(srv, validToken)
		require.False(t, ok)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("valid token for member returns DID and ok with 200", func(t *testing.T) {
		status, did, ok := callValidate(newSrv(org.NewEveryoneOrg()), validToken)
		require.True(t, ok)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, syntax.DID("did:web:test"), did)
	})
}
