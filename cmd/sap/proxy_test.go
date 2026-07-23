package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/stretchr/testify/require"
)

const (
	testProxyDID     = "did:plc:testorg"
	testProxySession = "session-1"
)

// futureJWT returns a JWT whose only meaningful claim is an expiry far in the
// future, so the OAuth transport treats the access token as valid and never
// attempts a refresh. The signature is irrelevant: expiry is read via
// ParseUnverified.
func futureJWT(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	signed, err := tok.SignedString([]byte("test-secret"))
	require.NoError(t, err)
	return signed
}

// testDPoPKey returns a valid DPoP private key multibase so ResumeSession can
// parse the fake session data.
func testDPoPKey(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return key.Multibase()
}

// openProxyTestServer wires up a sap server whose managed org points at the
// given pear host, returning an httptest server exposing the /proxy/ route.
func openProxyTestServer(t *testing.T, pearHost string) *httptest.Server {
	t.Helper()

	db := testutil.NewDB(t)

	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth.NewClientApp(&cfg, store)

	s, err := sap.NewSap(sap.SapConfig{DB: db, OAuthClient: oauthApp})
	require.NoError(t, err)

	require.NoError(t, store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              testProxyDID,
		SessionID:               testProxySession,
		HostURL:                 pearHost,
		AccessToken:             futureJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))

	server := NewSapServer(s, oauthApp)
	mux := http.NewServeMux()
	mux.HandleFunc("/proxy/", server.handleProxy)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)
	return httpServer
}

func TestServerProxyForwardsRequestToPear(t *testing.T) {
	t.Parallel()

	var gotReq *http.Request
	var gotBody string
	pear := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReq = r.Clone(r.Context())
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("X-Pear-Response", "ok")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"result":"created"}`))
	}))
	t.Cleanup(pear.Close)

	httpServer := openProxyTestServer(t, pear.URL)

	req, err := http.NewRequest(
		http.MethodPost,
		httpServer.URL+"/proxy/network.habitat.space.createRecord?collection=note",
		strings.NewReader(`{"text":"hi"}`),
	)
	require.NoError(t, err)
	req.Header.Set(habitatDIDHeader, testProxyDID)
	req.Header.Set(habitatSessionHeader, testProxySession)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// The response from pear is passed through unchanged.
	require.Equal(t, http.StatusCreated, resp.StatusCode)
	require.Equal(t, "ok", resp.Header.Get("X-Pear-Response"))
	body, _ := io.ReadAll(resp.Body)
	require.JSONEq(t, `{"result":"created"}`, string(body))

	// The request reached pear as an /xrpc/ call with method, query, and body intact.
	require.NotNil(t, gotReq)
	require.Equal(t, http.MethodPost, gotReq.Method)
	require.Equal(t, "/xrpc/network.habitat.space.createRecord", gotReq.URL.Path)
	require.Equal(t, "note", gotReq.URL.Query().Get("collection"))
	require.Equal(t, `{"text":"hi"}`, gotBody)
	require.Equal(t, "application/json", gotReq.Header.Get("Content-Type"))

	// The OAuth session's token is attached and the session selector headers
	// are stripped.
	require.NotEmpty(t, gotReq.Header.Get("Authorization"))
	require.Equal(t, "oauth", gotReq.Header.Get("Habitat-Auth-Method"))
	require.Empty(t, gotReq.Header.Get(habitatDIDHeader))
	require.Empty(t, gotReq.Header.Get(habitatSessionHeader))
}

func TestServerProxyRejectsMissingHeaders(t *testing.T) {
	t.Parallel()

	httpServer := openProxyTestServer(t, "http://unused.example")

	// No headers at all.
	resp, err := http.Get(httpServer.URL + "/proxy/network.habitat.space.listSpaces")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp.StatusCode)

	// DID without a session ID.
	req, err := http.NewRequest(
		http.MethodGet,
		httpServer.URL+"/proxy/network.habitat.space.listSpaces",
		nil,
	)
	require.NoError(t, err)
	req.Header.Set(habitatDIDHeader, testProxyDID)
	resp2, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp2.Body.Close() }()
	require.Equal(t, http.StatusBadRequest, resp2.StatusCode)
}

func TestServerProxyUnknownSessionReturnsBadGateway(t *testing.T) {
	t.Parallel()

	httpServer := openProxyTestServer(t, "http://unused.example")

	req, err := http.NewRequest(
		http.MethodGet,
		httpServer.URL+"/proxy/network.habitat.space.listSpaces",
		nil,
	)
	require.NoError(t, err)
	req.Header.Set(habitatDIDHeader, "did:plc:unknownorg")
	req.Header.Set(habitatSessionHeader, "nope")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusBadGateway, resp.StatusCode)
}
