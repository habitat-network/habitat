package oauthserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"

	"github.com/habitat-network/habitat/internal/authn"
	dbtestutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/fgastore"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/login"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/org/testutil"
	org_testutil "github.com/habitat-network/habitat/internal/org/testutil"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
)

// testStore creates a Store with a seeded test org.
func testStore(t *testing.T) org.Store {
	t.Helper()
	s := testutil.NewTestStore(t)
	_, _, err := s.CreateOrg(
		t.Context(),
		"org-name",
		"admin",
		"password",
		"",
		"",
		"",
		"contact@example.com",
	)
	require.NoError(t, err)
	return s
}

func TestOAuthServerErrorPaths(t *testing.T) {
	t.Run("NewOAuthServer rejects invalid secret", func(t *testing.T) {
		_, err := NewOAuthServer(
			[]byte("not valid base64"),
			nil, nil, nil, noop.Meter{}, testStore(t),
			"https://habitat.example",
			NewJWTBearerStore(),
		)
		require.Error(t, err)
	})

	// Common setup for all handler tests.
	db := dbtestutil.NewDB(t)
	secretStr, err := encrypt.GenerateKey()
	require.NoError(t, err)
	secret, err := encrypt.ParseKey(secretStr)
	require.NoError(t, err)
	oauthSrv, err := NewOAuthServer(
		secret,
		&org.LoginRouter{
			Pds:      login_testutil.NewPassthroughProvider(t),
			OrgStore: testStore(t),
		},
		pdsclient.NewDummyDirectory("http://pds.url"),
		db,
		noop.Meter{},
		testStore(t),
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthSrv.HandleAuthorize(w, r)
		case "/oauth-callback":
			oauthSrv.HandleCallback(w, r)
		case "/token":
			oauthSrv.HandleToken(w, r)
		case "/resource":
			oauthSrv.Validate(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	t.Run("CanHandle returns true for oauth header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{}).
			SignedString(secret)
		require.NoError(t, err)
		r.Header.Set("Authorization", "Bearer "+token)
		r.Header.Set("Habitat-Auth-Method", "oauth")
		require.True(t, oauthSrv.CanHandle(r))
	})

	t.Run("CanHandle returns false without oauth header", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		require.False(t, oauthSrv.CanHandle(r))
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
		resp, err := server.Client().Get(server.URL + "/oauth-callback")
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
	db := dbtestutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	pds := login_testutil.NewPassthroughProvider(t)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	oauthServer, err := NewOAuthServer(
		bytes,
		&org.LoginRouter{
			Pds: pds,
		},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
		case "/oauth-callback":
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
	pds.RedirectURI = server.URL + "/oauth-callback"

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
					RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
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
	config.RedirectURL = clientApp.URL + "/oauth-callback"

	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:example.did.com", nil)
	require.NoError(t, err)

	// CheckRedirect stops the client from following past the callback so we can
	// inspect its status code directly.
	server.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	// Drive the flow until it hits /oauth-callback — the server follows redirects
	// through the dummy PDS and stops at the first non-redirect from /oauth-callback.
	httpClient := server.Client()
	resp, err := httpClient.Do(authRequest)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// Follow redirects manually until we reach /oauth-callback.
	for resp.StatusCode == http.StatusSeeOther {
		loc := resp.Header.Get("Location")
		nextReq, reqErr := http.NewRequest(http.MethodGet, loc, nil)
		require.NoError(t, reqErr)
		resp, err = httpClient.Do(nextReq)
		require.NoError(t, err)
		_ = resp.Body.Close()
		if nextReq.URL.Path == "/oauth-callback" {
			break
		}
	}

	require.Equal(t, http.StatusSeeOther /* What fosite authorize error uses */, resp.StatusCode)
}

func TestOAuthServerE2E(t *testing.T) {
	// setup test database
	db := dbtestutil.NewDB(t)

	// Generate RSA key for JWT signing
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err, "failed to generate secret")
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	pds := login_testutil.NewPassthroughProvider(t)
	oauthServer, err := NewOAuthServer(
		bytes,
		&org.LoginRouter{
			Pds: pds,
		},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err, "failed to setup oauth server")

	// setup http server oauth client to make requests to
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
			return
		case "/oauth-callback":
			oauthServer.HandleCallback(w, r)
			return
		case "/token":
			oauthServer.HandleToken(w, r)
			return
		case "/resource":
			require.True(t, oauthServer.CanHandle(r))
			credInfo, ok := oauthServer.Validate(w, r)
			require.True(t, ok, "failed to validate token")
			require.Equal(t, syntax.DID("did:web:example.did.com"), credInfo.Subject)
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
	pds.RedirectURI = server.URL + "/oauth-callback"

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
					RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
					ResponseTypes: []string{"code"},
					GrantTypes:    []string{"authorization_code", "refresh_token"},
				})
				require.NoError(t, err, "failed to encode client metadata")
				return
			case "/oauth-callback":
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
	config.RedirectURL = clientApp.URL + "/oauth-callback"

	// create authorize request
	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:example.did.com", nil)
	require.NoError(t, err)

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

// TestOAuthServerAuthenticatesHiveServedIdentity drives the full authorization
// code flow for an identity minted and served entirely by hive (not by any
// public-facing did:web host or the PLC directory). It asserts that the OAuth
// server resolves the user's handle to a DID, and the DID back to an identity,
// using only hive's internal store — no DNS or HTTP-based identity resolution
// is configured or reachable for the domain used here.
func TestOAuthServerAuthenticatesHiveServedIdentity(t *testing.T) {
	// memberDomain deliberately isn't a resolvable public host: hive must be
	// able to serve this identity without any network-based did:web lookup.
	const memberDomain = "unreachable.invalid"
	const pearDomain = "pear." + memberDomain

	// hive and the org store share one db: org.WithTx swaps hive's db
	// connection for its own transaction when minting member identities.
	hiveDB := dbtestutil.NewDB(t)
	h, err := hive.NewHive(memberDomain, pearDomain, hiveDB)
	require.NoError(t, err, "failed to create hive")

	// The PDS-side directory only needs to resolve a DID to a PDS endpoint for
	// the OAuth handshake with the (dummy) PDS; it is independent of how the
	// OAuth server itself resolves a handle to a hive-served DID.
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")

	// The dummy PDS always issues tokens for "did:web:example.did.com" (see
	// DummyOAuthClient.ExchangeCode), so the org member is registered with
	// that as their external atproto login identity, while their actual
	// habitat identity (and the DID the OAuth server issues a token for) is
	// the hive-served one resolved from their handle.
	const pdsLoginDID = "did:web:example.did.com"

	passwordProvider, err := login.NewPasswordProvider(
		hiveDB,
		pearDomain,
		[]byte("test-signing-secret-for-org-00000"),
		dummyDir,
	)
	require.NoError(t, err, "failed to setup password provider")
	fgaStore, err := fgastore.NewMemory(t.Context())
	require.NoError(t, err, "failed to setup fga store")
	orgStore, err := org.NewStore(hiveDB, h, dummyDir, pearDomain, passwordProvider, fgaStore)
	require.NoError(t, err, "failed to setup org store")

	_, member, err := orgStore.CreateOrg(
		t.Context(),
		"org-name",
		"alice",
		"",
		"atproto",
		pdsLoginDID,
		"acme",
		"contact@example.com",
	)
	require.NoError(t, err, "failed to create org with hive-served admin")

	// setup test database
	db := dbtestutil.NewDB(t)

	// Generate RSA key for JWT signing
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err, "failed to generate secret")
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	pds := login_testutil.NewPassthroughProvider(t)
	oauthServer, err := NewOAuthServer(
		bytes,
		&org.LoginRouter{
			Pds:      pds,
			OrgStore: orgStore,
		},
		h, // the OAuth server resolves handles/DIDs via hive, not a public directory
		db,
		noop.Meter{},
		orgStore,
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err, "failed to setup oauth server")

	// setup http server oauth client to make requests to
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
			return
		case "/oauth-callback":
			oauthServer.HandleCallback(w, r)
			return
		case "/token":
			oauthServer.HandleToken(w, r)
			return
		case "/resource":
			credInfo, ok := oauthServer.Validate(w, r)
			require.True(t, ok, "failed to validate token")
			require.Equal(t, member.DID, credInfo.Subject)
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
	pds.RedirectURI = server.URL + "/oauth-callback"

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
					RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
					ResponseTypes: []string{"code"},
					GrantTypes:    []string{"authorization_code", "refresh_token"},
				})
				require.NoError(t, err, "failed to encode client metadata")
				return
			case "/oauth-callback":
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
	config.RedirectURL = clientApp.URL + "/oauth-callback"

	// create authorize request, identifying the user by their hive-served handle
	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle="+member.Handle.String(), nil)
	require.NoError(t, err)

	// make authorize requests which will follow redirects all the way to token response
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

	// use server as the oauth client http client because it has the tls cert
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
}

func TestHandleCallbackRejectsOrgScopeForNonAdmin(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	pds := login_testutil.NewPassthroughProvider(t)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	oauthServer, err := NewOAuthServer(
		bytes,
		&org.LoginRouter{
			Pds: pds,
		},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/authorize":
			oauthServer.HandleAuthorize(w, r)
		case "/oauth-callback":
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
	pds.RedirectURI = server.URL + "/oauth-callback"

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
					RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
					ResponseTypes: []string{"code"},
					GrantTypes:    []string{"authorization_code", "refresh_token"},
					Scope:         "org:*",
				}))
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	)
	defer clientApp.Close()

	config.ClientID = clientApp.URL + "/client-metadata.json"
	config.RedirectURL = clientApp.URL + "/oauth-callback"

	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:example.did.com&scope=org:*", nil)
	require.NoError(t, err)

	server.Client().CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	httpClient := server.Client()
	resp, err := httpClient.Do(authRequest)
	require.NoError(t, err)
	_ = resp.Body.Close()

	for resp.StatusCode == http.StatusSeeOther {
		loc := resp.Header.Get("Location")
		nextReq, reqErr := http.NewRequest(http.MethodGet, loc, nil)
		require.NoError(t, reqErr)
		resp, err = httpClient.Do(nextReq)
		require.NoError(t, err)
		_ = resp.Body.Close()
		if nextReq.URL.Path == "/oauth-callback" {
			break
		}
	}

	require.Equal(t, http.StatusSeeOther, resp.StatusCode)
}

// testIsMemberStore wraps an org.Store and overrides GetOrgByDID.
type testIsMemberStore struct {
	org.Store
	fn func(ctx context.Context, did syntax.DID) (org.Org, error)
}

func (s *testIsMemberStore) GetOrgForDID(
	ctx context.Context,
	did syntax.DID,
) (org.Org, bool, error) {
	o, err := s.fn(ctx, did)
	return o, false, err
}

// acquireAccessToken drives the full authorization code flow and returns the
// resulting bearer access token issued by srv.
func acquireAccessToken(
	t *testing.T,
	srv *OAuthServer,
	pds *login_testutil.PassthroughProvider,
) string {
	t.Helper()
	flowServer := httptest.NewTLSServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/authorize":
				srv.HandleAuthorize(w, r)
			case "/oauth-callback":
				srv.HandleCallback(w, r)
			case "/token":
				srv.HandleToken(w, r)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		}),
	)
	t.Cleanup(flowServer.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	flowServer.Client().Jar = jar
	pds.RedirectURI = flowServer.URL + "/oauth-callback"

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
				RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
				ResponseTypes: []string{"code"},
				GrantTypes:    []string{"authorization_code", "refresh_token"},
			}))
		case "/oauth-callback":
			ctx := context.WithValue(r.Context(), oauth2.HTTPClient, flowServer.Client())
			token, exchangeErr := oauthCfg.Exchange(
				ctx,
				r.URL.Query().Get("code"),
				oauth2.VerifierOption(verifier),
			)
			require.NoError(t, exchangeErr)
			capturedToken = token.AccessToken
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(clientApp.Close)

	oauthCfg.ClientID = clientApp.URL + "/client-metadata.json"
	oauthCfg.RedirectURL = clientApp.URL + "/oauth-callback"

	authReq, err := http.NewRequest(
		http.MethodGet,
		oauthCfg.AuthCodeURL(
			"test-state",
			oauth2.S256ChallengeOption(verifier),
		)+"&handle=did:web:example.did.com",
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
	db := dbtestutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	// newSrv creates an OAuthServer sharing the same secret and database.
	// Stateless JWT introspection means tokens issued by any server here are
	// valid for all others created with the same secret.
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	newSrv := func(st org.Store) (*OAuthServer, *login_testutil.PassthroughProvider) {
		p := login_testutil.NewPassthroughProvider(t)
		s, srvErr := NewOAuthServer(
			bytes,
			&org.LoginRouter{
				Pds: p,
			},
			dummyDir,
			db,
			noop.Meter{},
			st,
			"https://habitat.example",
			NewJWTBearerStore(),
		)
		require.NoError(t, srvErr)
		return s, p
	}

	// Issue a real JWT via the complete OAuth flow.
	srv, pds := newSrv(testStore(t))
	validToken := acquireAccessToken(t, srv, pds)

	// callValidate issues a GET against a minimal HTTP server wrapping srv.Validate
	// and returns the HTTP status code together with Validate's return values.
	callValidate := func(srv *OAuthServer, bearerToken string) (status int, did *authn.CredentialInfo, ok bool) {
		var (
			mu     sync.Mutex
			retDID *authn.CredentialInfo
			retOK  bool
		)
		httpSrv := httptest.NewServer(
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				d, o := srv.Validate(w, r)
				mu.Lock()
				retDID, retOK = d, o
				mu.Unlock()
			}),
		)
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
		srvEmpty, _ := newSrv(testStore(t))
		status, _, ok := callValidate(srvEmpty, "")
		require.False(t, ok)
		require.NotEqual(t, http.StatusOK, status)

		srvBad, _ := newSrv(testStore(t))
		status, _, ok = callValidate(srvBad, "not.a.valid.jwt")
		require.False(t, ok)
		require.NotEqual(t, http.StatusOK, status)
	})

	t.Run("GetOrgForDID error returns !ok with 401", func(t *testing.T) {
		srv, _ := newSrv(&testIsMemberStore{
			Store: testStore(t),
			fn: func(_ context.Context, _ syntax.DID) (org.Org, error) {
				return nil, errors.New("simulated database failure")
			},
		})
		status, _, ok := callValidate(srv, validToken)
		require.False(t, ok)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("non-member returns !ok with 401", func(t *testing.T) {
		srv, _ := newSrv(&testIsMemberStore{
			Store: testStore(t),
			fn: func(_ context.Context, _ syntax.DID) (org.Org, error) {
				return nil, org.ErrMemberNotFound
			},
		})
		status, _, ok := callValidate(srv, validToken)
		require.False(t, ok)
		require.Equal(t, http.StatusUnauthorized, status)
	})

	t.Run("valid token for member returns DID and ok with 200", func(t *testing.T) {
		srv, _ := newSrv(testStore(t))
		status, credInfo, ok := callValidate(srv, validToken)
		require.True(t, ok)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, syntax.DID("did:web:example.did.com"), credInfo.Subject)
	})
}

func TestValidateWithScopeChecking(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	newSrv := func(st org.Store) (*OAuthServer, *login_testutil.PassthroughProvider) {
		p := login_testutil.NewPassthroughProvider(t)
		s, srvErr := NewOAuthServer(
			bytes,
			&org.LoginRouter{
				Pds: p,
			},
			dummyDir,
			db,
			noop.Meter{},
			st,
			"https://habitat.example",
			NewJWTBearerStore(),
		)
		require.NoError(t, srvErr)
		return s, p
	}

	t.Run("token without org scope fails org scope requirement", func(t *testing.T) {
		srv, pds := newSrv(testStore(t))
		token := acquireAccessToken(t, srv, pds)
		_, _, err := srv.ValidateRaw(t.Context(), token, "org:*")
		require.Error(t, err)
	})

	t.Run("token passes with no scope requirement", func(t *testing.T) {
		srv, pds := newSrv(testStore(t))
		token := acquireAccessToken(t, srv, pds)
		_, _, err := srv.ValidateRaw(t.Context(), token)
		require.NoError(t, err)
	})

	t.Run("valid token returns DID and ok", func(t *testing.T) {
		srv, pds := newSrv(testStore(t))
		token := acquireAccessToken(t, srv, pds)
		credInfo, ok, err := srv.ValidateRaw(t.Context(), token)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, syntax.DID("did:web:example.did.com"), credInfo.Subject)
	})
}

// TestIndigoClientApp exercises the full OAuth 2.0 authorization code flow
// (PAR → authorize → callback → token → resource) using the indigo
// oauth.ClientApp. Unlike the existing TestOAuthServerE2E which uses Go's
// standard x/oauth2 library, this test drives the flow with indigo's DPoP-bound
// SendInitialTokenRequest, ResumeSession, and ClientSession.DoWithAuth.
func TestIndigoClientApp(t *testing.T) {
	// The disambiguation page returns a handle, which the OAuth server resolves
	// (via hive) to the member's hive-served DID; the org store then routes that
	// DID's login to the atproto (passthrough) provider. The passthrough stands
	// in for the PDS OAuth dance and reports the member's external atproto login
	// DID on exchange.
	const pdsLoginDID = "did:web:example.did.com"
	const memberDomain = "unreachable.invalid"
	const pearDomain = "pear." + memberDomain

	orgStore := org_testutil.NewTestStore(t)

	db := dbtestutil.NewDB(t)
	loginProvider := login_testutil.NewPassthroughProvider(t)
	loginProvider.LoginID = pdsLoginDID
	dir := pdsclient.NewDummyDirectory("https://habitat.example")
	oauthServer, err := NewOAuthServer(
		encrypt.TestKey,
		&org.LoginRouter{
			Pds:      loginProvider,
			OrgStore: orgStore,
		},
		dir,
		db,
		noop.Meter{},
		orgStore,
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err)

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Logf("server path: %s", r.URL.String())
		switch r.URL.Path {
		case "/oauth/par":
			oauthServer.HandlePAR(w, r)
		case "/oauth/authorize":
			oauthServer.HandleAuthorize(w, r)
		case "/ui/login/disambiguate":
			// Stand in for the disambiguation page + user: re-issue the
			// authorization request with the preserved params plus a handle.
			http.Redirect(w, r, "/oauth/authorize?handle=example.handle.com", http.StatusSeeOther)
		case "/oauth-callback":
			oauthServer.HandleCallback(w, r)
		case "/oauth/token":
			oauthServer.HandleToken(w, r)
		case "/resource":
			oauthServer.Validate(w, r)
		case "/.well-known/oauth-authorization-server":
			oauthServer.HandleAuthServerMetadata(w, r)
		case "/.well-known/oauth-protected-resource":
			oauthServer.HandleProtectedResourceMetadata(w, r)
		default:
			t.Logf("unknown server path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	loginProvider.RedirectURI = "https://habitat.example/oauth-callback"

	// Client app serves the client metadata document and echoes back the
	// fosite authorization code at its own callback URL.
	clientApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(&oauth.ClientMetadata{
				ClientID:      "http://" + r.Host + "/client-metadata.json",
				RedirectURIs:  []string{"http://" + r.Host + "/oauth-callback"},
				ResponseTypes: []string{"code"},
				GrantTypes:    []string{"authorization_code", "refresh_token"},
				Scope:         "atproto",
			})
			require.NoError(t, err)
		case "/oauth-callback":
			t.Logf("callback: %s", r.URL.String())
			w.Header().Set("Content-Type", "application/json")
			err := json.NewEncoder(w).Encode(map[string]string{
				"state":        r.URL.Query().Get("state"),
				"code":         r.URL.Query().Get("code"),
				"redirect_uri": r.URL.Query().Get("redirect_uri"),
				"iss":          r.URL.Query().Get("iss"),
				"scope":        r.URL.Query().Get("scope"),
			})
			require.NoError(t, err)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(clientApp.Close)

	// Build the indigo ClientApp with a public-client config and in-memory
	// store.  We override the HTTP client so that it trusts the test server's
	// TLS certificate and override the identity directory with our dummy so
	// that ProcessCallback (not called here but kept for consistency) can
	// resolve DIDs.
	indigoApp := oauth.NewClientApp(new(oauth.NewPublicConfig(
		clientApp.URL+"/client-metadata.json",
		clientApp.URL+"/oauth-callback",
		[]string{"atproto"},
	)), oauth.NewMemStore())

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	client := &http.Client{Jar: jar, Transport: &roundTripper{t: t, server: server}}
	indigoApp.Client = client
	indigoApp.Resolver.Client = client
	// indigo resolves the token subject DID to a PDS host and then to an auth
	// server; point every DID at the habitat issuer so that resolution matches.
	indigoApp.Dir = dir

	redirect, err := indigoApp.StartAuthFlow(
		t.Context(),
		"https://habitat.example?handle=example.handle.com",
	)
	require.NoError(t, err)

	resp, err := client.Get(redirect)
	require.NoError(t, err)
	var respJson map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&respJson))
	require.NoError(t, err, resp.Body.Close())
	t.Logf("respJson: %v", respJson)
	require.NotEmpty(t, respJson["code"])

	_, err = indigoApp.ProcessCallback(t.Context(), url.Values{
		"code":         []string{respJson["code"]},
		"state":        []string{respJson["state"]},
		"redirect_uri": []string{respJson["redirect_uri"]},
		"iss":          []string{respJson["iss"]},
	})
	require.NoError(t, err)
}

type roundTripper struct {
	t      *testing.T
	server *httptest.Server
}

func (rt *roundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	fmt.Fprintf(os.Stderr, "DEBUG roundTripper original URL=%s\n", req.URL.String())
	fmt.Fprintf(os.Stderr, "DEBUG roundTripper cookies=%q\n", req.Header.Get("Cookie"))
	if req.URL.Host == "habitat.example" {
		url, err := url.Parse(rt.server.URL + req.URL.RequestURI())
		require.NoError(rt.t, err)
		req.URL = url
		fmt.Fprintf(os.Stderr, "DEBUG roundTripper rewritten URL=%s\n", req.URL.String())
	}
	resp, err := rt.server.Client().Transport.RoundTrip(req)
	if err == nil {
		fmt.Fprintf(
			os.Stderr,
			"DEBUG roundTripper response status=%d set-cookie=%q\n",
			resp.StatusCode,
			resp.Header.Get("Set-Cookie"),
		)
	}
	return resp, err
}

// TestHandleAuthorizeDisambiguation exercises the disambiguation redirect: when
// an authorize request arrives without a handle, the server redirects to the
// disambiguation page; the page (simulated here) re-issues the request with
// a handle, and the flow completes normally.
func TestHandleAuthorizeDisambiguation(t *testing.T) {
	db := dbtestutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)

	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")
	pds := login_testutil.NewPassthroughProvider(t)
	oauthServer, err := NewOAuthServer(
		bytes,
		&org.LoginRouter{
			Pds: pds,
		},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		"https://habitat.example",
		NewJWTBearerStore(),
	)
	require.NoError(t, err)

	var disambiguateVisited bool
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/authorize":
			oauthServer.HandleAuthorize(w, r)
		case "/oauth-callback":
			oauthServer.HandleCallback(w, r)
		case "/oauth/token":
			oauthServer.HandleToken(w, r)
		case "/ui/login/disambiguate":
			disambiguateVisited = true
			params := r.URL.Query()
			params.Set("handle", "did:web:example.did.com")
			http.Redirect(w, r, "/oauth/authorize?"+params.Encode(), http.StatusSeeOther)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	jar, err := cookiejar.New(nil)
	require.NoError(t, err)
	server.Client().Jar = jar
	pds.RedirectURI = server.URL + "/oauth-callback"

	verifier := oauth2.GenerateVerifier()
	config := &oauth2.Config{
		Endpoint: oauth2.Endpoint{
			AuthURL:  server.URL + "/oauth/authorize",
			TokenURL: server.URL + "/oauth/token",
		},
	}

	var capturedToken string
	clientApp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
				ClientId:      "http://" + r.Host + "/client-metadata.json",
				RedirectUris:  []string{"http://" + r.Host + "/oauth-callback"},
				ResponseTypes: []string{"code"},
				GrantTypes:    []string{"authorization_code", "refresh_token"},
			}))
		case "/oauth-callback":
			ctx := context.WithValue(r.Context(), oauth2.HTTPClient, server.Client())
			token, exchangeErr := config.Exchange(
				ctx,
				r.URL.Query().Get("code"),
				oauth2.VerifierOption(verifier),
			)
			require.NoError(t, exchangeErr)
			capturedToken = token.AccessToken
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(map[string]string{
				"token": token.AccessToken,
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(clientApp.Close)

	config.ClientID = clientApp.URL + "/client-metadata.json"
	config.RedirectURL = clientApp.URL + "/oauth-callback"

	// Make an authorize request WITHOUT a handle — the server should redirect
	// to the disambiguation page, which re-issues the request with a handle,
	// and the full OAuth flow completes.
	authReq, err := http.NewRequest(
		http.MethodGet,
		config.AuthCodeURL("test-state", oauth2.S256ChallengeOption(verifier)),
		nil,
	)
	require.NoError(t, err)

	result, err := server.Client().Do(authReq)
	require.NoError(t, err)
	respBytes, err := io.ReadAll(result.Body)
	require.NoError(t, err)
	require.NoError(t, result.Body.Close())
	require.Equal(t, http.StatusOK, result.StatusCode, "authorize request failed: %s", respBytes)

	require.True(t, disambiguateVisited, "disambiguation page should have been visited")
	require.NotEmpty(t, capturedToken, "OAuth flow should complete after disambiguation")
}
