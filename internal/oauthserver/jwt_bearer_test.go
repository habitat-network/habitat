package oauthserver

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	login_testutil "github.com/habitat-network/habitat/internal/login/testutil"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
	oauthjwt "golang.org/x/oauth2/jwt"
)

// newJWTBearerTestClient creates an httptest server serving a
// client-metadata.json document with a generated RSA key pair, and returns an
// oauthjwt.Config pre-configured with that key and client metadata URL.
func newJWTBearerTestClient(t *testing.T) *oauthjwt.Config {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	privPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	})

	keyID := "test-key"
	var clientID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
				ClientId:   clientID,
				GrantTypes: []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"},
				Jwks: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
					Key:       privateKey.Public(),
					KeyID:     keyID,
					Algorithm: string(jose.RS256),
					Use:       "sig",
				}}},
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	clientID = server.URL + "/client-metadata.json"

	return &oauthjwt.Config{
		Email:        clientID,
		PrivateKey:   privPEM,
		PrivateKeyID: keyID,
		PrivateClaims: map[string]any{
			"jti": "test-jti",
		},
	}
}

// setupJWTBearerTestServer wires up an OAuthServer whose token endpoint is
// reachable at the returned tokenURL. The issuer parameter determines the
// expected aud claim for JWT Bearer assertions (issuer + "/oauth/token").
// approvedClientIDs are registered in the JWT Bearer client allow-list.
func setupJWTBearerTestServer(
	t *testing.T,
	issuer string,
	approvedClientIDs ...string,
) (srv *OAuthServer, actualTokenURL string) {
	t.Helper()
	db := testutil.NewDB(t)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")

	var oauthServer *OAuthServer
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			oauthServer.HandleToken(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	actualTokenURL = server.URL + "/token"
	oauthServer, err = NewOAuthServer(
		bytes,
		&org.LoginRouter{Pds: login_testutil.NewPassthroughProvider(t)},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		issuer,
		NewJWTBearerStore(approvedClientIDs...),
	)
	require.NoError(t, err)

	return oauthServer, actualTokenURL
}

func TestHandleTokenJWTBearerGrant(t *testing.T) {
	t.Run("issues an access token for an allow-listed client", func(t *testing.T) {
		cfg := newJWTBearerTestClient(t)
		clientURL, _ := url.Parse(cfg.Email)
		domain := clientURL.Host
		srv, tokenURL := setupJWTBearerTestServer(t, domain, cfg.Email)

		const subject = "did:web:service-subject.example"
		cfg.Subject = subject
		cfg.TokenURL = tokenURL
		cfg.Audience = domain + "/oauth/token"
		cfg.Expires = time.Minute
		tok, err := cfg.TokenSource(t.Context()).Token()
		require.NoError(t, err)
		require.NotEmpty(t, tok.AccessToken)

		did, ok, err := srv.ValidateRaw(t.Context(), tok.AccessToken)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, subject, did.Subject.String())
	})

	t.Run("rejects an assertion from a client not on the allow-list", func(t *testing.T) {
		_, tokenURL := setupJWTBearerTestServer(t, "example.com")
		cfg := newJWTBearerTestClient(t) // not added to the allow-list

		cfg.Subject = "did:web:subject.example"
		cfg.TokenURL = tokenURL
		cfg.Audience = "example.com/oauth/token"
		cfg.Expires = time.Minute
		_, err := cfg.TokenSource(t.Context()).Token()
		require.Error(t, err)
	})

	t.Run("rejects an assertion signed with a key not in the client's jwks", func(t *testing.T) {
		cfg := newJWTBearerTestClient(t)
		clientURL, _ := url.Parse(cfg.Email)
		domain := clientURL.Host
		_, tokenURL := setupJWTBearerTestServer(t, domain, cfg.Email)

		otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
		require.NoError(t, err)
		otherPEM := pem.EncodeToMemory(&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(otherKey),
		})
		badCfg := &oauthjwt.Config{
			Email:        cfg.Email,
			PrivateKey:   otherPEM,
			PrivateKeyID: cfg.PrivateKeyID,
			Subject:      "did:web:subject.example",
			TokenURL:     tokenURL,
			Audience:     domain + "/oauth/token",
			Expires:      time.Minute,
			PrivateClaims: map[string]any{
				"jti": "test-jti-mismatched",
			},
		}
		_, err = badCfg.TokenSource(t.Context()).Token()
		var retrieveErr *oauth2.RetrieveError
		if errors.As(err, &retrieveErr) {
			require.Equal(t, http.StatusBadRequest, retrieveErr.Response.StatusCode)
		} else {
			require.Error(t, err)
		}
	})

	t.Run("rejects an assertion with the wrong audience", func(t *testing.T) {
		cfg := newJWTBearerTestClient(t)
		clientURL, _ := url.Parse(cfg.Email)
		domain := clientURL.Host
		_, tokenURL := setupJWTBearerTestServer(t, domain, cfg.Email)

		cfg.Subject = "did:web:subject.example"
		cfg.TokenURL = tokenURL
		cfg.Audience = "https://wrong-audience.example"
		cfg.Expires = time.Minute
		_, err := cfg.TokenSource(t.Context()).Token()
		require.Error(t, err)
	})
}
