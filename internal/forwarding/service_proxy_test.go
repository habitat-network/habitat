package forwarding

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

func newTestServiceProxyHive(t *testing.T) hive.Hive {
	t.Helper()
	h, err := hive.NewHive("example.com", "pear.example.com", testutil.NewDB(t))
	require.NoError(t, err)
	return h
}

// neverNext is a handler that fails the test if called.
func neverNext(t *testing.T) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("next handler should not have been called")
	})
}

func TestServiceProxyNoHeader_CallsNext(t *testing.T) {
	sp := NewServiceProxy(authntest.NewFailMethod(), nil, identity.NewMockDirectory(), nil)

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	sp(next).ServeHTTP(w, r)

	require.True(t, nextCalled)
	require.Equal(t, http.StatusOK, w.Code)
}

func TestServiceProxyMalformedHeader_Returns400(t *testing.T) {
	sp := NewServiceProxy(
		authntest.NewSuccessMethod(syntax.DID("did:web:alice.org.example.com")),
		nil,
		identity.NewMockDirectory(),
		nil,
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	r.Header.Set("Atproto-Proxy", "did:web:labeler.example.com") // missing #serviceId
	sp(neverNext(t)).ServeHTTP(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServiceProxyAuthFails_Returns401(t *testing.T) {
	sp := NewServiceProxy(authntest.NewFailMethod(), nil, identity.NewMockDirectory(), nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	r.Header.Set("Atproto-Proxy", "did:web:labeler.example.com#atproto_labeler")
	sp(neverNext(t)).ServeHTTP(w, r)

	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestServiceProxyDIDResolutionFails_Returns502(t *testing.T) {
	// Empty directory — LookupDID will not find the target DID.
	sp := NewServiceProxy(
		authntest.NewSuccessMethod(syntax.DID("did:web:alice.org.example.com")),
		nil,
		identity.NewMockDirectory(),
		nil,
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	r.Header.Set("Atproto-Proxy", "did:web:labeler.example.com#atproto_labeler")
	sp(neverNext(t)).ServeHTTP(w, r)

	require.Equal(t, http.StatusBadGateway, w.Code)
}

func TestServiceProxyServiceNotFound_Returns400(t *testing.T) {
	const targetDID = "did:web:labeler.example.com"
	dir := identity.NewMockDirectory()
	// DID is registered but does not have the requested service.
	dir.Insert(identity.Identity{
		DID: syntax.DID(targetDID),
		Services: map[string]identity.ServiceEndpoint{
			"other_service": {URL: "https://other.example.com"},
		},
	})
	sp := NewServiceProxy(
		authntest.NewSuccessMethod(syntax.DID("did:web:alice.org.example.com")),
		nil,
		dir,
		nil,
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	r.Header.Set("Atproto-Proxy", targetDID+"#atproto_labeler")
	sp(neverNext(t)).ServeHTTP(w, r)

	require.Equal(t, http.StatusBadRequest, w.Code)
}

// TestServiceProxyIntegration_ForwardsWithServiceAuth verifies the full proxy flow:
// the middleware resolves the target DID, signs a service auth JWT on the caller's
// behalf, and forwards the request stripping DPoP and Atproto-Proxy headers.
func TestServiceProxyIntegration_ForwardsWithServiceAuth(t *testing.T) {
	h := newTestServiceProxyHive(t)
	callerID, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)

	// Record the forwarded request for assertions.
	var received *http.Request
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Clone(context.Background())
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)

	const targetDID = "did:web:labeler.example.com"
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      syntax.DID(targetDID),
		Services: map[string]identity.ServiceEndpoint{"atproto_labeler": {URL: target.URL}},
	})

	sp := NewServiceProxy(authntest.NewSuccessMethod(callerID.DID), h, dir, nil)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	r.Header.Set("Atproto-Proxy", targetDID+"#atproto_labeler")
	r.Header.Set("DPoP", "some-dpop-proof")
	sp(neverNext(t)).ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
	require.NotNil(t, received, "forwarded request was not received by target")
	require.Empty(
		t,
		received.Header.Get("Atproto-Proxy"),
		"Atproto-Proxy must be stripped to prevent forwarding loops",
	)
	require.Empty(
		t,
		received.Header.Get("DPoP"),
		"DPoP must be stripped (proof is bound to Habitat's endpoint)",
	)

	// Verify the forwarded Authorization is a service auth JWT issued by the caller.
	auth := received.Header.Get("Authorization")
	require.True(t, strings.HasPrefix(auth, "Bearer "), "Authorization must be a Bearer token")
	token := strings.TrimPrefix(auth, "Bearer ")
	p := jwt.NewParser(jwt.WithoutClaimsValidation())
	claims := &jwt.MapClaims{}
	_, _, err = p.ParseUnverified(token, claims)
	require.NoError(t, err)
	iss, err := claims.GetIssuer()
	require.NoError(t, err)
	require.Equal(t, callerID.DID.String(), iss)
	aud, err := claims.GetAudience()
	require.NoError(t, err)
	require.Contains(t, aud, targetDID)
}

func TestServiceProxyIntegration_RemoteDID(t *testing.T) {
	h := newTestServiceProxyHive(t)
	calledDID := syntax.DID("did:plc:12345")
	targetDID := "did:web:labeler.example.com"
	// Record the forwarded request for assertions.
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("Atproto-Proxy"))
		require.Empty(t, r.Header.Get("DPoP"))
		token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		require.True(t, found)
		require.Equal(t, "token", token)
	}))

	pds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/com.atproto.server.getServiceAuth", r.URL.Path)
		require.Equal(t, "did:web:labeler.example.com#atproto_labeler", r.URL.Query().Get("aud"))
		require.Equal(t, "app.bsky.feed.getTimeline", r.URL.Query().Get("lxm"))
		json.NewEncoder(w).Encode(map[string]any{
			"token": "token",
		})
	}))

	t.Cleanup(target.Close)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      syntax.DID(targetDID),
		Services: map[string]identity.ServiceEndpoint{"atproto_labeler": {URL: target.URL}},
	})

	sp := NewServiceProxy(
		authntest.NewSuccessMethod(calledDID),
		h,
		dir,
		pdsclient.NewDummyClientFactory(pds.URL),
	)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	r.Header.Set("Atproto-Proxy", targetDID+"#atproto_labeler")
	r.Header.Set("DPoP", "some-dpop-proof")
	sp(neverNext(t)).ServeHTTP(w, r)

	require.Equal(t, http.StatusOK, w.Code)
}
