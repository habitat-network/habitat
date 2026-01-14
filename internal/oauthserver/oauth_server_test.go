package oauthserver_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/eagraf/habitat-new/internal/encrypt"
	"github.com/eagraf/habitat-new/internal/oauthclient"
	"github.com/eagraf/habitat-new/internal/oauthserver"
	"github.com/eagraf/habitat-new/internal/pdscred"
	"github.com/go-jose/go-jose/v3"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestOAuthServerE2E(t *testing.T) {
	// setup test database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err, "failed to open test database")

	// setup pds credential store
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err, "failed to setup pds credential store")

	// setup oauth server
	clientMetadata := &oauthclient.ClientMetadata{}
	oauthClient := oauthserver.NewDummyOAuthClient(t, clientMetadata)
	defer oauthClient.Close()

	// Generate RSA key for JWT signing
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err, "failed to generate RSA key")

	oauthServer := oauthserver.NewOAuthServer(
		&jose.JSONWebKey{
			Key:       privateKey,
			KeyID:     "test-key",
			Algorithm: string(jose.RS256),
			Use:       "sig",
		},
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		oauthclient.NewDummyDirectory("http://pds.url"),
		credStore,
	)

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
				err := json.NewEncoder(w).Encode(&oauthclient.ClientMetadata{
					ClientId:      "http://" + r.Host + "/client-metadata.json",
					RedirectUris:  []string{"http://" + r.Host + "/callback"},
					ResponseTypes: []string{"code"},
					GrantTypes:    []string{"authorization_code"},
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
	require.Equal(t, http.StatusOK, result.StatusCode, "authorize request failed: %s", respBytes)

	token := &oauth2.Token{}
	require.NoError(t, json.Unmarshal(respBytes, token), "failed to decode token")
	require.NotEmpty(t, token.AccessToken, "access token should not be empty")

	// use server as the oauth client http client because of it has the tls cert
	oauthClientCtx := context.WithValue(context.Background(), oauth2.HTTPClient, server.Client())
	client := config.Client(oauthClientCtx, token)

	resp, err := client.Get(server.URL + "/resource")
	require.NoError(t, err, "failed to make resource request")
	respBytes, err = io.ReadAll(resp.Body)
	require.NoError(t, err, "failed to read response body")
	require.NoError(t, resp.Body.Close())
	require.Equal(t, http.StatusOK, resp.StatusCode, "resource request failed: %s", respBytes)
}
