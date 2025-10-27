package oauthserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"github.com/eagraf/habitat-new/internal/auth"
	"github.com/eagraf/habitat-new/internal/oauthserver"
	"github.com/gorilla/securecookie"
	"github.com/gorilla/sessions"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

func TestOAuthServerE2E(t *testing.T) {
	// setup oauth server
	serverMetadata := &auth.ClientMetadata{}
	oauthClient := oauthserver.NewDummyOAuthClient(t, serverMetadata)
	defer oauthClient.Close()

	oauthServer := oauthserver.NewOAuthServer(
		oauthClient,
		sessions.NewCookieStore(securecookie.GenerateRandomKey(32)),
		auth.NewDummyDirectory("http://pds.url"),
	)
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
		default:
			t.Errorf("unknown server path: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// set the server's oauthClient redirectUri now that we know the url
	serverMetadata.RedirectUris = []string{server.URL + "/callback"}

	// setup client app
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
				err := json.NewEncoder(w).Encode(&auth.ClientMetadata{
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

	// Set the client app's OAuth configuration not that we know the url
	config.ClientID = clientApp.URL + "/client-metadata.json"
	config.RedirectURL = clientApp.URL + "/callback"

	authRequest, err := http.NewRequest(http.MethodGet, config.AuthCodeURL(
		"test-state",
		oauth2.S256ChallengeOption(verifier),
	)+"&handle=did:web:test", nil)
	require.NoError(t, err, "failed to create authorize request")

	jar, err := cookiejar.New(nil)
	require.NoError(t, err, "failed to create cookie jar")
	server.Client().Jar = jar

	result, err := server.Client().Do(authRequest)
	defer func() { require.NoError(t, result.Body.Close()) }()
	require.NoError(t, err, "failed to make authorize request")
	require.Equal(t, http.StatusOK, result.StatusCode)

	token := &oauth2.Token{}
	require.NoError(t, json.NewDecoder(result.Body).Decode(token), "failed to decode token")

	require.NotEmpty(t, token.AccessToken, "access token should not be empty")
}
