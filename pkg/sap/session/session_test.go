package session

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/oauthclient"
)

func testDPoPKey(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return key.Multibase()
}

func testJWT(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tok, err := jwt.NewWithClaims(jwt.SigningMethodPS256,
		jwt.MapClaims{"exp": time.Now().Add(time.Hour).Unix(), "jti": "test"},
	).SignedString(key)
	require.NoError(t, err)
	return tok
}

// fakeDirectory resolves each DID's habitat host from a fixed map, standing
// in for identity.Directory / credential.Directory.
type fakeDirectory map[syntax.DID]string

func (f fakeDirectory) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	host, ok := f[did]
	if !ok {
		return nil, fmt.Errorf("did %s not found", did)
	}
	return &identity.Identity{
		DID:      did,
		Services: map[string]identity.ServiceEndpoint{"habitat": {URL: host}},
	}, nil
}

func TestStoreSessionsAndSpaceAccess(t *testing.T) {
	t.Parallel()
	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))

	oauthStore, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	s := NewStore(db, oauth.NewClientApp(&cfg, oauthStore), nil, nil)

	require.NoError(t, oauthStore.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              "did:plc:alice",
		SessionID:               "sess1",
		HostURL:                 "https://host.example",
		AccessToken:             testJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))
	require.NoError(t, s.Add(t.Context(), "did:plc:alice", "sess1", AuthOAuth))

	dids, err := s.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []string{"did:plc:alice"}, []string{dids[0].String()})

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, s.RecordSpaceAccess(t.Context(), space, "did:plc:alice"))
	require.NoError(t, s.RecordSpaceAccess(t.Context(), space, "did:plc:alice")) // idempotent

	spaces, err := s.Spaces(t.Context())
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{space}, spaces)

	// ClientForSpace never fails for lack of a session up front: the
	// credential is minted lazily on first use (see
	// TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost).
	client, err := s.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	require.NotNil(t, client)

	require.NoError(t, s.DropSpace(t.Context(), space))
	spaces, err = s.Spaces(t.Context())
	require.NoError(t, err)
	require.Empty(t, spaces)
}

type fakeJWTClients struct {
	client *http.Client
	calls  int
}

func (f *fakeJWTClients) ClientForDID(ctx context.Context, did syntax.DID) (*http.Client, error) {
	f.calls++
	return f.client, nil
}

func TestClientForSessionDispatchesJWTBearer(t *testing.T) {
	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	sentinel := &http.Client{}
	jwt := &fakeJWTClients{client: sentinel}
	store := NewStore(db, nil, jwt, nil)

	did := syntax.DID("did:web:member.example")
	require.NoError(t, store.Add(t.Context(), did, "", AuthJWTBearer))

	got, err := store.ClientForSession(t.Context(), did)
	require.NoError(t, err)
	require.Same(t, sentinel, got)
	require.Equal(t, 1, jwt.calls)
}

// memberAuthTransport stands in for a jwt-bearer session's own act-as-subject
// client: it resolves relative URLs against base and attaches a fixed
// member-level bearer token, the same auth an OAuth session's access token
// would provide.
type memberAuthTransport struct{ base string }

func (t memberAuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if !req.URL.IsAbs() {
		u, err := url.Parse(t.base)
		if err != nil {
			return nil, err
		}
		req.URL.Scheme, req.URL.Host = u.Scheme, u.Host
	}
	req.Header.Set("Authorization", "Bearer member-tok")
	return http.DefaultTransport.RoundTrip(req)
}

// TestClientForSpaceDispatchesJWTBearer verifies a repo-host read for a space
// only a jwt-bearer session can access mints a space credential the same way
// an OAuth session does: getDelegationToken is authorized with the session's
// own act-as-subject token, and the resulting delegation token is exchanged
// for a space credential used on the actual repo-host read. ClientForSpace
// backs every syncer read (listRepoOps, getRepo, ...) and, per the
// permissioned-data proposal, listRepos, so this is what makes those work
// for a jwt-bearer-only session.
func TestClientForSpaceDispatchesJWTBearer(t *testing.T) {
	var delegationAuth, credentialAuth, repoAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getDelegationToken":
			delegationAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetDelegationTokenOutput{Token: "deleg-tok"})
		case "/xrpc/network.habitat.space.getSpaceCredential":
			credentialAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		case "/xrpc/network.habitat.space.listRepos":
			repoAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	memberClient := &http.Client{Transport: memberAuthTransport{base: srv.URL}}
	jwt := &fakeJWTClients{client: memberClient}
	did := syntax.DID("did:web:member.example")
	// The session is also the space owner here, so one host serves both the
	// member-auth and space-credential legs; see
	// TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost for the
	// case where they differ.
	dir := fakeDirectory{did: srv.URL}
	store := NewStore(db, nil, jwt, dir)

	require.NoError(t, store.Add(t.Context(), did, "", AuthJWTBearer))

	space := habitat_syntax.SpaceURI("at://did:web:member.example/space/network.habitat.group/s1")
	require.NoError(t, store.RecordSpaceAccess(t.Context(), space, did))

	client, err := store.ClientForSpace(t.Context(), space)
	require.NoError(t, err)

	resp, err := client.Get("/xrpc/network.habitat.space.listRepos")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, "Bearer member-tok", delegationAuth)
	require.Equal(t, "Bearer deleg-tok", credentialAuth)
	require.Equal(t, "Bearer space-cred", repoAuth)
}

// TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost verifies the
// credential exchange and the actual repo-host read land on the space
// owner's own host — resolved from the space URI, since a space's records
// live in its owner's repo — even though the delegating session (the only
// one with recorded access) lives on a completely different host. Getting
// this wrong means the credential exchange, and every read it backs, targets
// a host that has never heard of the space.
func TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost(t *testing.T) {
	var delegationAuth, credentialAuth, repoAuth string

	memberSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/xrpc/network.habitat.space.getDelegationToken" {
			t.Errorf("unexpected path on member host: %s", r.URL.Path)
			return
		}
		delegationAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(
			habitat.NetworkHabitatSpaceGetDelegationTokenOutput{Token: "deleg-tok"})
	}))
	t.Cleanup(memberSrv.Close)

	ownerSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/xrpc/network.habitat.space.getSpaceCredential":
			credentialAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(
				habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: "space-cred"})
		case "/xrpc/network.habitat.space.listRepos":
			repoAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListReposOutput{})
		default:
			t.Errorf("unexpected path on owner host: %s", r.URL.Path)
		}
	}))
	t.Cleanup(ownerSrv.Close)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	member := syntax.DID("did:web:member.example")
	owner := syntax.DID("did:web:owner.example")
	memberClient := &http.Client{Transport: memberAuthTransport{base: memberSrv.URL}}
	jwt := &fakeJWTClients{client: memberClient}
	dir := fakeDirectory{owner: ownerSrv.URL}
	store := NewStore(db, nil, jwt, dir)

	require.NoError(t, store.Add(t.Context(), member, "", AuthJWTBearer))
	space := habitat_syntax.SpaceURI("at://" + owner.String() + "/space/network.habitat.group/s1")
	require.NoError(t, store.RecordSpaceAccess(t.Context(), space, member))

	client, err := store.ClientForSpace(t.Context(), space)
	require.NoError(t, err)

	resp, err := client.Get("/xrpc/network.habitat.space.listRepos")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.Equal(t, "Bearer member-tok", delegationAuth)
	require.Equal(t, "Bearer deleg-tok", credentialAuth)
	require.Equal(t, "Bearer space-cred", repoAuth)
}
