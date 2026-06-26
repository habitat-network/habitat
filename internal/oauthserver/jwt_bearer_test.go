package oauthserver

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/login"
	"github.com/habitat-network/habitat/internal/org"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/habitat-network/habitat/internal/pdscred"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
	"golang.org/x/oauth2"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// jwtBearerTestClient serves a client-metadata.json document with the given
// public key, and signs assertions with the matching private key.
type jwtBearerTestClient struct {
	clientID   string
	privateKey *ecdsa.PrivateKey
	keyID      string
	grantTypes []string
	server     *httptest.Server
}

func newJWTBearerTestClient(t *testing.T) *jwtBearerTestClient {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	c := &jwtBearerTestClient{
		privateKey: privateKey,
		keyID:      "test-key",
		grantTypes: []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"},
	}
	c.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/client-metadata.json":
			w.Header().Set("Content-Type", "application/json")
			require.NoError(t, json.NewEncoder(w).Encode(&pdsclient.ClientMetadata{
				ClientId:   c.clientID,
				GrantTypes: c.grantTypes,
				Jwks: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
					Key:       privateKey.Public(),
					KeyID:     c.keyID,
					Algorithm: string(jose.ES256),
					Use:       "sig",
				}}},
			}))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(c.server.Close)
	c.clientID = c.server.URL + "/client-metadata.json"
	return c
}

// assertion signs a JWT Bearer grant assertion (RFC 7523 §2.1) for subject,
// asserting aud and jti as given, using signingKey (defaults to the client's
// own key when nil, to allow constructing assertions with a mismatched key).
func (c *jwtBearerTestClient) assertion(
	t *testing.T,
	subject string,
	aud string,
	jti string,
	signingKey *ecdsa.PrivateKey,
) string {
	t.Helper()
	if signingKey == nil {
		signingKey = c.privateKey
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: signingKey},
		&jose.SignerOptions{ExtraHeaders: map[jose.HeaderKey]interface{}{"kid": c.keyID}},
	)
	require.NoError(t, err)
	token, err := jwt.Signed(signer).Claims(&jwt.Claims{
		Issuer:   c.clientID,
		Subject:  subject,
		Audience: jwt.Audience{aud},
		Expiry:   jwt.NewNumericDate(time.Now().Add(time.Minute)),
		IssuedAt: jwt.NewNumericDate(time.Now()),
		ID:       jti,
	}).CompactSerialize()
	require.NoError(t, err)
	return token
}

// setupJWTBearerTestServer wires up an OAuthServer whose token endpoint is
// reachable at the returned tokenURL. The domain parameter controls the
// acceptable aud claim ("https://"+domain+"/oauth/token") and the JWT Bearer
// client allow-list.
func setupJWTBearerTestServer(
	t *testing.T,
	domain string,
) (srv *OAuthServer, tokenURL string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	credStore, err := pdscred.NewPDSCredentialStore(db, encrypt.TestKey)
	require.NoError(t, err)
	secret, err := encrypt.GenerateKey()
	require.NoError(t, err)
	bytes, err := encrypt.ParseKey(secret)
	require.NoError(t, err)
	dummyDir := pdsclient.NewDummyDirectory("http://pds.url")

	// The handler closure reads oauthServer only once a request arrives, by
	// which point it has been assigned below.
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

	tokenURL = server.URL + "/token"
	oauthClient := pdsclient.NewDummyOAuthClient(t, &pdsclient.ClientMetadata{})
	t.Cleanup(oauthClient.Close)
	oauthServer, err = NewOAuthServer(
		bytes,
		&org.LoginRouter{Pds: login.NewPDSProvider(oauthClient, credStore, dummyDir)},
		dummyDir,
		db,
		noop.Meter{},
		testStore(t),
		domain,
	)
	require.NoError(t, err)

	return oauthServer, tokenURL
}

func postJWTBearerToken(t *testing.T, tokenURL string, assertion string) *http.Response {
	t.Helper()
	resp, err := http.Post(
		tokenURL,
		"application/x-www-form-urlencoded",
		strings.NewReader(url.Values{
			"grant_type": []string{"urn:ietf:params:oauth:grant-type:jwt-bearer"},
			"assertion":  []string{assertion},
		}.Encode()),
	)
	require.NoError(t, err)
	return resp
}

func TestHandleTokenJWTBearerGrant(t *testing.T) {
	t.Run("issues an access token for an allow-listed client", func(t *testing.T) {
		client := newJWTBearerTestClient(t)
		clientURL, _ := url.Parse(client.clientID)
		domain := clientURL.Host
		srv, tokenURL := setupJWTBearerTestServer(t, domain)

		const subject = "did:web:service-subject.example"
		resp := postJWTBearerToken(
			t,
			tokenURL,
			client.assertion(t, subject, "https://"+domain+"/oauth/token", "jti-1", nil),
		)
		defer func() { _ = resp.Body.Close() }()
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.Equal(t, http.StatusOK, resp.StatusCode, "response: %s", body)

		token := &oauth2.Token{}
		require.NoError(t, json.Unmarshal(body, token))
		require.NotEmpty(t, token.AccessToken)

		did, ok, err := srv.ValidateRaw(t.Context(), token.AccessToken)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, subject, did.String())
	})

	t.Run("rejects an assertion from a client not on the allow-list", func(t *testing.T) {
		_, tokenURL := setupJWTBearerTestServer(t, "example.com")
		client := newJWTBearerTestClient(t) // not added to the allow-list

		resp := postJWTBearerToken(
			t,
			tokenURL,
			client.assertion(
				t,
				"did:web:subject.example",
				"https://example.com/oauth/token",
				"jti-2",
				nil,
			),
		)
		defer func() { _ = resp.Body.Close() }()
		require.NotEqual(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("rejects an assertion signed with a key not in the client's jwks", func(t *testing.T) {
		client := newJWTBearerTestClient(t)
		clientURL, _ := url.Parse(client.clientID)
		domain := clientURL.Host
		_, tokenURL := setupJWTBearerTestServer(t, domain)

		otherKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)

		resp := postJWTBearerToken(
			t,
			tokenURL,
			client.assertion(
				t,
				"did:web:subject.example",
				"https://"+domain+"/oauth/token",
				"jti-3",
				otherKey,
			),
		)
		defer func() { _ = resp.Body.Close() }()
		require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})

	t.Run("rejects an assertion with the wrong audience", func(t *testing.T) {
		client := newJWTBearerTestClient(t)
		clientURL, _ := url.Parse(client.clientID)
		domain := clientURL.Host
		_, tokenURL := setupJWTBearerTestServer(t, domain)

		resp := postJWTBearerToken(
			t,
			tokenURL,
			client.assertion(
				t, "did:web:subject.example", "https://wrong-audience.example", "jti-5", nil,
			),
		)
		defer func() { _ = resp.Body.Close() }()
		require.NotEqual(t, http.StatusOK, resp.StatusCode)
	})
}
