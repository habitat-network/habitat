package credential

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/clientmetadata"
	"github.com/habitat-network/habitat/internal/httpsig"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// stubDelegator hands out a fixed delegation token.
type stubDelegator struct{}

func (stubDelegator) DelegationToken(context.Context, habitat_syntax.SpaceURI) (string, error) {
	return "test-delegation", nil
}

// credToken extracts the credential a ClientForSpace client would send.
func credToken(t *testing.T, m *Manager, space habitat_syntax.SpaceURI) string {
	t.Helper()
	client, err := m.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	return strings.TrimPrefix(
		client.Headers.Get("Authorization"), httpsig.CredentialScheme+" ",
	)
}

// dirWithSpaceHost builds an identity.MockDirectory with a single identity
// whose atproto_pds service (its space host) points at host.
func dirWithSpaceHost(did syntax.DID, host string) *identity.MockDirectory {
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      did,
		Services: map[string]identity.ServiceEndpoint{"atproto_pds": {URL: host}},
	})
	return dir
}

// testAttester builds a confidential *oauth.ClientConfig with a freshly
// generated key, suitable for any Manager under test — every real caller
// (sap always runs as a confidential OAuth client) supplies one of these.
func testAttester(t *testing.T, clientID string) *oauth.ClientConfig {
	t.Helper()
	attester := &oauth.ClientConfig{ClientID: clientID}
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	require.NoError(t, attester.SetClientSecret(key, "test-kid"))
	return attester
}

// TestManagerMintsCachesAndAuthenticates covers mint (against the space
// owner's resolved host, not any fixed host), cache (no second mint on a hot
// call), and a repo-host request carrying the credential.
func TestManagerMintsCachesAndAuthenticates(t *testing.T) {
	var (
		mu           sync.Mutex
		credCalls    int
		repoAuth     string
		repoRevCalls int
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getSpaceCredential":
			mu.Lock()
			credCalls++
			mu.Unlock()
			// The delegation token travels in the body; the request carries
			// no auth of its own.
			require.Empty(t, r.Header.Get("Authorization"))
			var in habitat.NetworkHabitatSpaceGetSpaceCredentialInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
			require.Equal(t, "test-delegation", in.DelegationToken)
			require.True(t, httpsig.IsP256DIDKey(in.CredentialKid))
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		case "/xrpc/network.habitat.space.listRepos":
			mu.Lock()
			repoAuth = r.Header.Get("Authorization")
			repoRevCalls++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	owner := syntax.DID("did:web:org")
	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	dir := dirWithSpaceHost(owner, srv.URL)
	m := NewManager(dir, srv.Client(), stubDelegator{}, testAttester(t, "https://sap.example.com"))

	require.Equal(t, "space-cred", credToken(t, m, space))

	// Cached: a second call does not re-mint.
	require.Equal(t, "space-cred", credToken(t, m, space))
	mu.Lock()
	require.Equal(t, 1, credCalls)
	mu.Unlock()

	// A repo-host read through the manager's client carries the credential.
	client, err := m.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	var out habitat.NetworkHabitatSpaceListReposOutput
	require.NoError(t, client.Get(
		t.Context(),
		"network.habitat.space.listRepos",
		map[string]any{"space": space.String()},
		&out,
	))
	mu.Lock()
	require.Equal(t, "Atproto-Space-Credential space-cred", repoAuth)
	require.Equal(t, 1, repoRevCalls)
	mu.Unlock()

	// DropSpace evicts; the next mint re-exchanges.
	m.DropSpace(space)
	require.Equal(t, "space-cred", credToken(t, m, space))
	mu.Lock()
	require.Equal(t, 2, credCalls)
	mu.Unlock()
}

// TestManagerResolvesHostPerSpace verifies each space's credential is
// exchanged against its own owner's host, not a host fixed at construction —
// two spaces owned by different DIDs land on two different test servers.
func TestManagerResolvesHostPerSpace(t *testing.T) {
	newServer := func(label string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/xrpc/network.habitat.space.getSpaceCredential" {
				t.Fatalf("unexpected path %s", r.URL.Path)
			}
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "cred-" + label})
		}))
	}
	srvA := newServer("a")
	t.Cleanup(srvA.Close)
	srvB := newServer("b")
	t.Cleanup(srvB.Close)

	ownerA := syntax.DID("did:web:org-a")
	ownerB := syntax.DID("did:web:org-b")
	spaceA := habitat_syntax.SpaceURI("at://did:web:org-a/space/network.habitat.group/s1")
	spaceB := habitat_syntax.SpaceURI("at://did:web:org-b/space/network.habitat.group/s1")
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      ownerA,
		Services: map[string]identity.ServiceEndpoint{"atproto_pds": {URL: srvA.URL}},
	})
	dir.Insert(identity.Identity{
		DID:      ownerB,
		Services: map[string]identity.ServiceEndpoint{"atproto_pds": {URL: srvB.URL}},
	})
	m := NewManager(
		dir,
		http.DefaultClient,
		stubDelegator{},
		testAttester(t, "https://sap.example.com"),
	)

	require.Equal(t, "cred-a", credToken(t, m, spaceA))
	require.Equal(t, "cred-b", credToken(t, m, spaceB))
}

// TestManagerReportsDeletedSpace verifies a host answering SpaceDeleted is
// reported through OnSpaceDeleted and ErrSpaceDeleted, and that any other
// failure says nothing about the space.
func TestManagerReportsDeletedSpace(t *testing.T) {
	errName := "SpaceDeleted"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": errName})
	}))
	t.Cleanup(srv.Close)

	owner := syntax.DID("did:web:org")
	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	m := NewManager(
		dirWithSpaceHost(owner, srv.URL),
		http.DefaultClient,
		stubDelegator{},
		testAttester(t, "https://sap.example.com"),
	)
	var reported []habitat_syntax.SpaceURI
	m.OnSpaceDeleted(func(_ context.Context, s habitat_syntax.SpaceURI) {
		reported = append(reported, s)
	})

	_, err := m.ClientForSpace(t.Context(), space)
	require.ErrorIs(t, err, ErrSpaceDeleted)
	require.Equal(t, []habitat_syntax.SpaceURI{space}, reported)

	errName = "NotAuthorized"
	_, err = m.ClientForSpace(t.Context(), space)
	require.Error(t, err)
	require.NotErrorIs(t, err, ErrSpaceDeleted)
	require.Len(t, reported, 1)
}

// TestManagerAttachesClientAttestation verifies mint signs a
// getSpaceCredential request's clientAttestation with a JWT that a real
// space authority (clientmetadata's verifier) actually accepts.
func TestManagerAttachesClientAttestation(t *testing.T) {
	owner := syntax.DID("did:web:org")
	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")

	var clientAttestation string
	mux := http.NewServeMux()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	clientID := srv.URL + "/client-metadata.json"
	attester := testAttester(t, clientID)

	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		jwks := attester.PublicJWKS()
		_ = json.NewEncoder(w).Encode(oauth.ClientMetadata{ClientID: clientID, JWKS: &jwks})
	})
	mux.HandleFunc(
		"/xrpc/network.habitat.space.getSpaceCredential",
		func(w http.ResponseWriter, r *http.Request) {
			var in habitat.NetworkHabitatSpaceGetSpaceCredentialInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
			clientAttestation = in.ClientAttestation
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		},
	)

	dir := dirWithSpaceHost(owner, srv.URL)
	m := NewManager(dir, srv.Client(), stubDelegator{}, attester)
	require.Equal(t, "space-cred", credToken(t, m, space))

	require.NotEmpty(t, clientAttestation)
	verifiedClientID, err := clientmetadata.VerifyAttestation(
		t.Context(), clientmetadata.NewResolver(), clientAttestation, owner,
	)
	require.NoError(t, err)
	require.Equal(t, clientID, verifiedClientID)
}

// unsignedCred builds a credential whose exp the manager reads; the signature
// is irrelevant because only the space host verifies it.
func unsignedCred(t *testing.T, exp time.Time) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, jwt.MapClaims{"exp": exp.Unix()}).
		SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)
	return tok
}

// TestManagerRenewsShortLivedCredentials verifies the cache honors the
// credential's own exp: one inside the renewal lead is re-minted.
func TestManagerRenewsShortLivedCredentials(t *testing.T) {
	var mu sync.Mutex
	var credCalls int
	var exp time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		credCalls++
		e := exp
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{
			Credential: unsignedCred(t, e),
		})
	}))
	t.Cleanup(srv.Close)

	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	m := NewManager(
		dirWithSpaceHost("did:web:org", srv.URL), srv.Client(), stubDelegator{},
		testAttester(t, "https://sap.example.com"),
	)

	exp = time.Now().Add(10 * time.Minute)
	credToken(t, m, space)
	credToken(t, m, space)
	require.Equal(t, 1, credCalls, "a fresh 10 minute credential is cached")

	exp = time.Now().Add(time.Minute)
	m.DropSpace(space)
	credToken(t, m, space)
	credToken(t, m, space)
	require.Equal(t, 3, credCalls, "a credential inside the renewal lead is re-minted")
}

// TestManagerRefetchesRejectedCredential verifies a space host's 401 (expired
// or revoked credential) triggers one re-fetch and retry.
func TestManagerRefetchesRejectedCredential(t *testing.T) {
	var mu sync.Mutex
	var minted int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getSpaceCredential":
			minted++
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{
				Credential: unsignedCred(
					t,
					time.Now().Add(10*time.Minute).Add(time.Duration(minted)*time.Second),
				),
			})
		case "/xrpc/network.habitat.space.listRepos":
			// Only the second credential is accepted; the first is "revoked".
			if minted < 2 {
				http.Error(w, `{"error":"InvalidToken"}`, http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		}
	}))
	t.Cleanup(srv.Close)

	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	m := NewManager(
		dirWithSpaceHost("did:web:org", srv.URL), srv.Client(), stubDelegator{},
		testAttester(t, "https://sap.example.com"),
	)
	client, err := m.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	var out habitat.NetworkHabitatSpaceListReposOutput
	require.NoError(t, client.Get(
		t.Context(), "network.habitat.space.listRepos",
		map[string]any{"space": space.String()}, &out,
	))
	require.Equal(t, 2, minted)
}

// TestManagerSignsRequestsWithBindingKey checks that the credential is
// requested for the manager's P-256 key (credentialKid) and that every use of
// it is RFC 9421-signed by that same key over the credential and audience.
func TestManagerSignsRequestsWithBindingKey(t *testing.T) {
	var credentialKid string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getSpaceCredential":
			var in habitat.NetworkHabitatSpaceGetSpaceCredentialInput
			require.NoError(t, json.NewDecoder(r.Body).Decode(&in))
			credentialKid = in.CredentialKid
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		case "/xrpc/network.habitat.space.listRepos":
			require.NoError(t, httpsig.VerifyBound(
				r, credentialKid, "authorization", "atproto-space-audience",
			))
			require.Equal(t, "did:web:org", r.Header.Get(httpsig.AudienceHeader))
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	owner := syntax.DID("did:web:org")
	space := habitat_syntax.SpaceURI("at://did:web:org/space/network.habitat.group/s1")
	m := NewManager(
		dirWithSpaceHost(owner, srv.URL), srv.Client(), stubDelegator{},
		testAttester(t, "https://sap.example.com"),
	)

	client, err := m.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	var out habitat.NetworkHabitatSpaceListReposOutput
	require.NoError(t, client.Get(
		t.Context(), "network.habitat.space.listRepos",
		map[string]any{"space": space.String()}, &out,
	))
	require.NotEmpty(t, credentialKid)
}
