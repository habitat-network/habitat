package session

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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

// fakeClients implements Clients directly from a fixed DID->client map,
// standing in for whatever the caller wires up around its own session
// storage (see cmd/sap/sessions.go for the real thing).
type fakeClients map[syntax.DID]*http.Client

func (f fakeClients) ClientForDID(_ context.Context, did syntax.DID) (*http.Client, error) {
	c, ok := f[did]
	if !ok {
		return nil, fmt.Errorf("no client for %s", did)
	}
	return c, nil
}

// roundTripFunc adapts a function to http.RoundTripper.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

// authedClient returns an *http.Client that resolves path-only requests
// against host and attaches a fixed bearer token, standing in for a real
// resumed OAuth/JWT-bearer session's authenticated client.
func authedClient(host, token string) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if !req.URL.IsAbs() {
			base, err := url.Parse(host)
			if err != nil {
				return nil, err
			}
			req.URL.Scheme = base.Scheme
			req.URL.Host = base.Host
		}
		req.Header.Set("Authorization", "Bearer "+token)
		return http.DefaultTransport.RoundTrip(req)
	})}
}

func TestStoreSessionsAndSpaceAccess(t *testing.T) {
	t.Parallel()
	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))

	s := NewStore(db, fakeClients{}, nil)

	require.NoError(t, s.Add(t.Context(), "did:plc:alice"))

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

// TestClientForSpaceUsesAccessingSessionForDelegation verifies a repo-host
// read for a space is authorized end-to-end: DelegationToken asks an
// accessing session's own client for a delegation token, which is then
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
	did := syntax.DID("did:web:member.example")
	// The session is also the space owner here, so one host serves both the
	// member-auth and space-credential legs; see
	// TestClientForSpaceUsesSpaceOwnerHostNotDelegatingSessionHost for the
	// case where they differ.
	dir := fakeDirectory{did: srv.URL}
	clients := fakeClients{did: authedClient(srv.URL, "member-tok")}
	store := NewStore(db, clients, dir)

	require.NoError(t, store.Add(t.Context(), did))

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
	clients := fakeClients{member: authedClient(memberSrv.URL, "member-tok")}
	dir := fakeDirectory{owner: ownerSrv.URL}
	store := NewStore(db, clients, dir)

	require.NoError(t, store.Add(t.Context(), member))
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
