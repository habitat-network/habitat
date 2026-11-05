package syncer

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// kemIdentity is a DID document publishing pub as #atproto_kem, with its PDS
// at pds.
func kemIdentity(t *testing.T, d syntax.DID, pds string, pub atcrypto.PublicKey) identity.Identity {
	t.Helper()
	ident := identity.Identity{
		DID:    d,
		Handle: syntax.HandleInvalid,
		Keys:   map[string]identity.VerificationMethod{},
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: pds},
		},
	}
	if pub != nil {
		ident.Keys[spacecommit.KEMFragment] = identity.VerificationMethod{
			Type: "Multikey", PublicKeyMultibase: pub.Multibase(),
		}
	}
	return ident
}

func newKEMKey(t *testing.T) *atcrypto.PrivateKeyP256 {
	t.Helper()
	k, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return k
}

func publicKey(t *testing.T, k *atcrypto.PrivateKeyP256) atcrypto.PublicKey {
	t.Helper()
	pub, err := k.PublicKey()
	require.NoError(t, err)
	return pub
}

// TestVerifierKEMKeySelectsAuthorOrHost covers the KEM key choice: the
// author's own key when the host is their PDS, the host's did:web key
// otherwise.
func TestVerifierKEMKeySelectsAuthorOrHost(t *testing.T) {
	t.Parallel()

	authorKey, hostKey := newKEMKey(t), newKEMKey(t)
	dir := identity.NewMockDirectory()
	author := syntax.DID("did:plc:alice")
	dir.Insert(kemIdentity(t, author, "https://pear.example.com", publicKey(t, authorKey)))
	hostDID := syntax.DID("did:web:host.example.com%3A8443")
	dir.Insert(kemIdentity(t, hostDID, "https://host.example.com:8443", publicKey(t, hostKey)))
	v := NewVerifier(dir)

	got, err := v.kemKey(t.Context(), "https://PEAR.example.com/", author)
	require.NoError(t, err)
	require.Equal(t, publicKey(t, authorKey).Multibase(), got.Multibase())

	got, err = v.kemKey(t.Context(), "https://host.example.com:8443", author)
	require.NoError(t, err)
	require.Equal(t, publicKey(t, hostKey).Multibase(), got.Multibase())

	_, err = v.kemKey(t.Context(), "https://other.example.com", author)
	require.ErrorContains(t, err, "lookup host did:web:other.example.com")

	_, err = v.kemKey(t.Context(), "https://pear.example.com", "did:plc:bob")
	require.ErrorContains(t, err, "lookup author did:plc:bob")

	dir.Insert(kemIdentity(t, "did:plc:nokem", "https://pear.example.com", nil))
	_, err = v.kemKey(t.Context(), "https://pear.example.com", "did:plc:nokem")
	require.ErrorContains(t, err, "kem key")

	_, err = v.kemKey(t.Context(), "://bad", author)
	require.ErrorContains(t, err, "parse host url")
}

// TestVerifierNilDegrades covers hash-only mode: no exchange, no Want-Mac
// header, and verification passes without a MAC.
func TestVerifierNilDegrades(t *testing.T) {
	t.Parallel()

	for _, v := range []*Verifier{nil, NewVerifier(nil)} {
		x, err := v.exchange(t.Context(), "https://h", "did:plc:a", spacecommit.CommitMACType)
		require.NoError(t, err)
		require.Nil(t, x)
		require.Empty(t, x.wantMAC())
		require.NoError(t, x.verifyCommit(http.Header{}, spacecommit.Commit{}, "", ""))
	}
}

// macHost is a fake repo host serving listRepoOps for one repo, MACing the
// head commit under the shared secret derived with whichever KEM key kem
// holds.
type macHost struct {
	kem      atomic.Pointer[atcrypto.PrivateKeyP256]
	commit   spacecommit.Commit
	requests atomic.Int32
	// tamper flips the MAC so it never verifies.
	tamper bool
}

func (h *macHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.requests.Add(1)
	keyShare, ok, err := spacecommit.ParseWantMAC(r.Header, spacecommit.CommitMACType)
	if err != nil || !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "BadSpaceMac"})
		return
	}
	secret, err := spacecommit.Decapsulate(h.kem.Load(), keyShare)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "BadSpaceMac"})
		return
	}
	mac := h.commit.MAC(secret)
	if h.tamper {
		mac[0] ^= 0xff
	}
	if err := spacecommit.SetMAC(w.Header(), spacecommit.CommitMACType, mac); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	c := h.commit.ToXRPC()
	_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{Commit: &c})
}

// rotatingDir is a directory whose Purge publishes the next KEM key, as a
// fresh resolution after a rotation would.
type rotatingDir struct {
	*identity.MockDirectory
	onPurge func()
}

func (d *rotatingDir) Purge(ctx context.Context, a syntax.AtIdentifier) error {
	if d.onPurge != nil {
		d.onPurge()
	}
	return d.MockDirectory.Purge(ctx, a)
}

// setupMACSync starts h and an engine whose verifier resolves the author with
// pub as their #atproto_kem and h as their PDS, and tracks one syncing repo.
func setupMACSync(
	t *testing.T,
	h *macHost,
	pub atcrypto.PublicKey,
) (*Engine, *rotatingDir, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	base, err := url.Parse(srv.URL)
	require.NoError(t, err)

	dir := &rotatingDir{MockDirectory: identity.NewMockDirectory()}
	dir.Insert(kemIdentity(t, h.commit.Repo, srv.URL, pub))

	db := db_testutil.NewDB(t, Models())
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e, err := New(db, fakeClients{base: base}, &memEmitter{}, NewVerifier(dir), 1, m)
	require.NoError(t, err)
	require.NoError(t, e.Track(t.Context(), h.commit.Space, h.commit.Repo))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ?", h.commit.Space).Update("state", stateSyncing).Error)
	return e, dir, srv
}

// stateOf is the tracked state of c's repo.
func stateOf(t *testing.T, e *Engine, c spacecommit.Commit) repoState {
	t.Helper()
	var r repo
	require.NoError(t, e.db.First(&r, "space = ? AND did = ?", c.Space, c.Repo).Error)
	return r.State
}

func macTestCommit() spacecommit.Commit {
	var lt spacecommit.LtHash
	return spacecommit.New(
		habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1"),
		"did:plc:alice", syntax.NewTIDClock(0).Next().String(), lt.Sum(),
	)
}

// TestEngineSyncRepoVerifiesCommitMAC covers the authenticated happy path: the
// head commit's MAC verifies under the negotiated secret and the repo settles.
func TestEngineSyncRepoVerifiesCommitMAC(t *testing.T) {
	t.Parallel()

	key := newKEMKey(t)
	h := &macHost{commit: macTestCommit()}
	h.kem.Store(key)
	e, _, _ := setupMACSync(t, h, publicKey(t, key))

	require.NoError(t, e.syncRepo(t.Context(), h.commit.Space, h.commit.Repo))
	require.Equal(t, stateActive, stateOf(t, e, h.commit))
	require.EqualValues(t, 1, h.requests.Load())
}

// TestEngineSyncRepoMACMismatchDesyncs covers a MAC that never verifies: sap
// retries once with a refreshed key, then marks the repo desynced.
func TestEngineSyncRepoMACMismatchDesyncs(t *testing.T) {
	t.Parallel()

	key := newKEMKey(t)
	h := &macHost{commit: macTestCommit(), tamper: true}
	h.kem.Store(key)
	e, _, _ := setupMACSync(t, h, publicKey(t, key))

	require.NoError(t, e.syncRepo(t.Context(), h.commit.Space, h.commit.Repo))
	require.Equal(t, stateDesynced, stateOf(t, e, h.commit))
	require.EqualValues(t, 2, h.requests.Load())
}

// TestEngineSyncRepoRetriesAfterKEMRotation covers a host whose KEM key
// rotated after sap resolved it: the first MAC fails, the DID document is
// re-resolved, and the retry verifies.
func TestEngineSyncRepoRetriesAfterKEMRotation(t *testing.T) {
	t.Parallel()

	oldKey, newKey := newKEMKey(t), newKEMKey(t)
	h := &macHost{commit: macTestCommit()}
	h.kem.Store(newKey)
	e, dir, srv := setupMACSync(t, h, publicKey(t, oldKey))
	dir.onPurge = func() {
		dir.Insert(kemIdentity(t, h.commit.Repo, srv.URL, publicKey(t, newKey)))
	}

	require.NoError(t, e.syncRepo(t.Context(), h.commit.Space, h.commit.Repo))
	require.Equal(t, stateActive, stateOf(t, e, h.commit))
	require.EqualValues(t, 2, h.requests.Load())
}
