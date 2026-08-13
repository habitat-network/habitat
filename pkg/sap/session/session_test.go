package session

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/oauthclient"
)

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

// newOAuthApp builds a bare (non-confidential) *oauth.ClientApp over a fresh
// GORM session store, for tests that only need ResumeSession to work.
func newOAuthApp(t *testing.T, db *gorm.DB) *oauth.ClientApp {
	t.Helper()
	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)
	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	return oauth.NewClientApp(&cfg, store)
}

func TestStoreSessionsAndSpaceAccess(t *testing.T) {
	t.Parallel()
	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))

	app := newOAuthApp(t, db)
	require.NoError(t, app.Store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              "did:plc:alice",
		SessionID:               "sess1",
		HostURL:                 "https://host.example",
		AccessToken:             testJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))

	s := NewStore(db, app, nil)
	require.NoError(t, s.Add(t.Context(), "did:plc:alice", "sess1"))

	sessions, err := s.List(t.Context())
	require.NoError(t, err)
	require.Equal(t, []Session{{DID: "did:plc:alice", SessionID: "sess1"}}, sessions)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, s.RecordSpaceAccess(t.Context(), space, "did:plc:alice", "sess1"))
	require.NoError(
		t,
		s.RecordSpaceAccess(t.Context(), space, "did:plc:alice", "sess1"),
	) // idempotent

	spaces, err := s.Spaces(t.Context())
	require.NoError(t, err)
	require.Equal(t, []habitat_syntax.SpaceURI{space}, spaces)

	// ClientForSpace never fails for lack of a session up front: the
	// credential is minted lazily on first use (see
	// TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost).
	spaceClient, err := s.ClientForSpace(t.Context(), space)
	require.NoError(t, err)
	require.NotNil(t, spaceClient)

	require.NoError(t, s.DropSpace(t.Context(), space))
	spaces, err = s.Spaces(t.Context())
	require.NoError(t, err)
	require.Empty(t, spaces)
}

// TestClientForSpaceUsesAccessingSessionForDelegation verifies a repo-host
// read for a space is authorized end-to-end: DelegationToken resumes an
// accessing session and asks it for a delegation token, which is then
// exchanged for a space credential used on the actual repo-host read.
// ClientForSpace backs every syncer read (listRepoOps, getRepo, ...) and,
// per the permissioned-data proposal, listRepos.
func TestClientForSpaceUsesAccessingSessionForDelegation(t *testing.T) {
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
	app := newOAuthApp(t, db)
	did := syntax.DID("did:web:member.example")
	require.NoError(t, app.Store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              did,
		SessionID:               "sess1",
		HostURL:                 srv.URL,
		AccessToken:             testJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))
	// The session is also the space owner here, so one host serves both the
	// member-auth and space-credential legs; see
	// TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost for the
	// case where they differ.
	dir := fakeDirectory{did: srv.URL}
	store := NewStore(db, app, dir)

	require.NoError(t, store.Add(t.Context(), did, "sess1"))

	space := habitat_syntax.SpaceURI("at://did:web:member.example/space/network.habitat.group/s1")
	require.NoError(t, store.RecordSpaceAccess(t.Context(), space, did, "sess1"))

	client, err := store.ClientForSpace(t.Context(), space)
	require.NoError(t, err)

	resp, err := client.Get("/xrpc/network.habitat.space.listRepos")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	// The member-auth leg uses DPoP, not a predictable literal token, so we
	// only assert something was sent; the credential exchange legs use fixed
	// bearer tokens minted by the (fake) space host.
	require.NotEmpty(t, delegationAuth)
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
	app := newOAuthApp(t, db)
	member := syntax.DID("did:web:member.example")
	owner := syntax.DID("did:web:owner.example")
	require.NoError(t, app.Store.SaveSession(t.Context(), oauth.ClientSessionData{
		AccountDID:              member,
		SessionID:               "sess1",
		HostURL:                 memberSrv.URL,
		AccessToken:             testJWT(t),
		DPoPPrivateKeyMultibase: testDPoPKey(t),
	}))
	dir := fakeDirectory{owner: ownerSrv.URL}
	store := NewStore(db, app, dir)

	require.NoError(t, store.Add(t.Context(), member, "sess1"))
	space := habitat_syntax.SpaceURI("at://" + owner.String() + "/space/network.habitat.group/s1")
	require.NoError(t, store.RecordSpaceAccess(t.Context(), space, member, "sess1"))

	client, err := store.ClientForSpace(t.Context(), space)
	require.NoError(t, err)

	resp, err := client.Get("/xrpc/network.habitat.space.listRepos")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.NotEmpty(t, delegationAuth)
	require.Equal(t, "Bearer deleg-tok", credentialAuth)
	require.Equal(t, "Bearer space-cred", repoAuth)
}
