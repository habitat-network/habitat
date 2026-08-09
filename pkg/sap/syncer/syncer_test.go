package syncer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// rewriteTransport routes path-only request URLs to a test server, standing in
// for the OAuth client transport.
type rewriteTransport struct{ base *url.URL }

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.URL.Scheme = t.base.Scheme
	req.URL.Host = t.base.Host
	return http.DefaultTransport.RoundTrip(req)
}

type fakeClients struct{ base *url.URL }

func (f fakeClients) ClientForSpace(
	context.Context,
	habitat_syntax.SpaceURI,
) (*http.Client, error) {
	return &http.Client{Transport: rewriteTransport(f)}, nil
}

// memEmitter collects emitted records in memory.
type memEmitter struct {
	mu      sync.Mutex
	emitted []habitat_syntax.SpaceRecordURI
}

func (e *memEmitter) Emit(
	_ context.Context,
	uri habitat_syntax.SpaceRecordURI,
	_ []byte,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, uri)
	return nil
}

func (e *memEmitter) InTx(*gorm.DB) Emitter { return e }

func newTestEngine(t *testing.T, hostURL string) (*Engine, *memEmitter, *gorm.DB) {
	t.Helper()
	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	base, err := url.Parse(hostURL)
	require.NoError(t, err)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	emitter := &memEmitter{}
	e := New(db, fakeClients{base: base}, emitter, NewVerifier(nil), 1, m)
	e.jobs = make(chan job, 100)
	return e, emitter, db
}

// TestEngineSyncRepoVerifiesAndSettles covers the incremental happy path: ops
// fold into the LtHash, the head commit's hash matches, and the repo settles
// active with its rev and hash state persisted.
func TestEngineSyncRepoVerifiesAndSettles(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	clock := syntax.NewTIDClock(0)
	rev1, rev2 := clock.Next().String(), clock.Next().String()

	ops := []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
		{Rev: rev1, Collection: "network.habitat.test", Rkey: "k1", Cid: "bafyaaa",
			Value: map[string]any{"n": 1}},
		{Rev: rev2, Collection: "network.habitat.test", Rkey: "k2", Cid: "bafybbb",
			Value: map[string]any{"n": 2}},
	}
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("network.habitat.test", "k1", "bafyaaa"))
	lt.Add(spacecommit.RecordElement("network.habitat.test", "k2", "bafybbb"))
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  rev2,
		Hash: base64.StdEncoding.EncodeToString(lt.Sum()),
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.listRepoOps", r.URL.Path)
		out := habitat.NetworkHabitatSpaceListRepoOpsOutput{Commit: commit}
		if r.URL.Query().Get("since") == "" {
			out.Ops = ops
			out.Cursor = rev2
		}
		_ = json.NewEncoder(w).Encode(out)
	}))
	t.Cleanup(srv.Close)

	e, emitter, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ?", space).Update("state", stateSyncing).Error)

	require.NoError(t, e.syncRepo(t.Context(), space, repoDID))

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, stateActive, r.State)
	require.Equal(t, syntax.TID(rev2), r.Rev)
	require.Equal(t, lt.State(), r.Hash)
	require.Len(t, emitter.emitted, 2)
}

// TestEngineNotifyWriteRequeues covers the notification happy path: an unknown
// repo starts tracking, and a settled repo behind the notified rev is
// requeued.
func TestEngineNotifyWriteRequeues(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")

	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:new", "aaa", nil))
	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:new").Error)
	require.Equal(t, statePending, r.State)

	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:new").
		Updates(map[string]any{"state": stateActive, "rev": "aaa"}).Error)
	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:new", "bbb", nil))
	require.NoError(t, db.First(&r, "did = ?", "did:plc:new").Error)
	require.Equal(t, statePending, r.State)
}

// TestEngineNewDefaults tests that New normalizes a non-positive parallelism.
func TestEngineNewDefaults(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e := New(db, fakeClients{base: &url.URL{}}, &memEmitter{}, NewVerifier(nil), 0, m)
	require.Equal(t, 5, e.parallelism)
}

// TestEngineWithTx verifies WithTx returns a new engine sharing the same
// channels and notifier but with the provided transaction.
func TestEngineWithTx(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	orig := New(db, fakeClients{base: &url.URL{}}, &memEmitter{}, NewVerifier(nil), 1, m)

	tx := db.Begin()
	defer tx.Rollback()
	scoped := orig.WithTx(tx)

	require.Equal(t, orig.notif, scoped.notif)
	require.Equal(t, orig.jobs, scoped.jobs)
	require.Equal(t, orig.emitter, scoped.emitter)
}

// TestEngineDropSpace tests that DropSpace removes all repos for a space.
func TestEngineDropSpace(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, e.Track(t.Context(), space, "did:plc:bob"))

	var count int64
	require.NoError(t, db.Model(&repo{}).Where("space = ?", space).Count(&count).Error)
	require.Equal(t, int64(2), count)

	require.NoError(t, e.DropSpace(t.Context(), space))

	require.NoError(t, db.Model(&repo{}).Where("space = ?", space).Count(&count).Error)
	require.Equal(t, int64(0), count)
}

// TestEngineNotifyWriteSyncingMarksDirty verifies that a notification arriving
// while a repo is syncing marks it dirty for requeue.
func TestEngineNotifyWriteSyncingMarksDirty(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateSyncing).Error)

	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:alice", "zzz", nil))

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.True(t, r.Dirty)
	require.Equal(t, stateSyncing, r.State)
}

// TestEngineNotifyWriteAlreadyBehindHash tests requeue when the notified hash
// differs even if the rev is the same.
func TestEngineNotifyWriteAlreadyBehindHash(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Updates(map[string]any{"state": stateActive, "rev": "aaa"}).Error)

	notifyHash := []byte{0x01, 0x02}
	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:alice", "aaa", notifyHash))

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, statePending, r.State)
}

// TestVerifierNilDir verifies hash-only mode when the verifier has no identity
// directory.
func TestVerifierNilDir(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("net.test", "r1", "cid1"))
	commit := spacecommit.SignedCommit{
		Ver:  spacecommit.Version,
		Hash: lt.Sum(),
		Rev:  "3kzl6abcde02k",
	}

	v := NewVerifier(nil)
	require.NoError(t, v.Verify(t.Context(), space, "did:plc:alice", commit, &lt))
}

// TestVerifierNilDirMismatch verifies that a hash mismatch is caught in
// hash-only mode.
func TestVerifierNilDirMismatch(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("net.test", "r1", "cid1"))
	commit := spacecommit.SignedCommit{
		Ver:  spacecommit.Version,
		Hash: []byte{0xff, 0xfe},
		Rev:  "3kzl6abcde02k",
	}

	v := NewVerifier(nil)
	err := v.Verify(t.Context(), space, "did:plc:alice", commit, &lt)
	require.Error(t, err)
	require.ErrorIs(t, err, spacecommit.ErrInvalidCommit)
}

// TestVerifierNilPointer tests that a nil Verifier falls back to hash-only.
func TestVerifierNilPointer(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("net.test", "r1", "cid1"))
	commit := spacecommit.SignedCommit{
		Ver:  spacecommit.Version,
		Hash: lt.Sum(),
		Rev:  "3kzl6abcde02k",
	}

	var v *Verifier
	require.NoError(t, v.Verify(t.Context(), space, "did:plc:alice", commit, &lt))
}

// TestDecodeBytesFieldCoversLexiconJSON covers the decodeBytesField and
// decodeCommit paths used by syncRepo.
func TestDecodeBytesFieldCoversLexiconJSON(t *testing.T) {
	t.Parallel()

	b, err := decodeBytesField(nil)
	require.NoError(t, err)
	require.Nil(t, b)

	b, err = decodeBytesField(base64.StdEncoding.EncodeToString([]byte{1, 2}))
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2}, b)

	_, err = decodeBytesField(42)
	require.Error(t, err)
}

// TestDecodeCommitFromLexicon covers decodeCommit with a full signed commit.
func TestDecodeCommitFromLexicon(t *testing.T) {
	t.Parallel()

	hashBytes := []byte{0xaa, 0xbb}
	lexicon := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  "3kzl6abcde02k",
		Hash: base64.StdEncoding.EncodeToString(hashBytes),
		Ikm:  base64.StdEncoding.EncodeToString([]byte{0x01}),
		Mac:  base64.StdEncoding.EncodeToString([]byte{0x02}),
		Sig:  base64.StdEncoding.EncodeToString([]byte{0x03}),
	}
	c, err := decodeCommit(lexicon)
	require.NoError(t, err)
	require.Equal(t, spacecommit.Version, c.Ver)
	require.Equal(t, hashBytes, c.Hash)
	require.Equal(t, []byte{0x01}, c.Ikm)
	require.Equal(t, []byte{0x02}, c.Mac)
	require.Equal(t, []byte{0x03}, c.Sig)
	require.Equal(t, "3kzl6abcde02k", c.Rev)
}

// TestDecodeCommitBadBase64 covers error paths in decodeCommit.
func TestDecodeCommitBadBase64(t *testing.T) {
	t.Parallel()

	_, err := decodeCommit(habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  1,
		Hash: "!!!notbase64!!!",
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "decode commit hash")
}

// TestEngineScheduleRetry covers the retry backoff path.
func TestEngineScheduleRetry(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateSyncing).Error)

	err := e.scheduleRetry(t.Context(), space, "did:plc:alice", stateError, fmt.Errorf("test error"))
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateError, r.State)
	require.Equal(t, "test error", r.ErrorMsg)
	require.Equal(t, 1, r.RetryCount)
	require.True(t, r.RetryAfter > 0)
}

// TestEngineSettleDirtyRepo verifies that settling a dirty repo requeues it
// as pending instead of active.
func TestEngineSettleDirtyRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Updates(map[string]any{"state": stateSyncing, "dirty": true}).Error)

	var lt spacecommit.LtHash
	err := e.settle(t.Context(), db, space, "did:plc:alice", "3kzl6abcde02k", lt.State())
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, statePending, r.State)
	require.False(t, r.Dirty)
}

// TestEngineRecoverRepoClientError covers the path where ClientForSpace fails
// in recoverRepo. scheduleRetry swallows the error, so we verify the repo
// remains desynced with an updated retry state.
func TestEngineRecoverRepoClientError(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	failClient := &failClients{}
	e := New(db, failClient, &memEmitter{}, NewVerifier(nil), 1, m)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateDesynced).Error)

	// recoverRepo calls scheduleRetry which returns nil.
	err = e.recoverRepo(t.Context(), space, "did:plc:alice")
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateDesynced, r.State)
	require.Equal(t, 1, r.RetryCount)
	require.NotEmpty(t, r.ErrorMsg)
}

// TestEngineRecoverRepoNonOK covers the path where getRepo returns a non-200.
func TestEngineRecoverRepoNonOK(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e := New(db, fakeClients{base: base}, &memEmitter{}, NewVerifier(nil), 1, m)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateDesynced).Error)

	err = e.recoverRepo(t.Context(), space, "did:plc:alice")
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateDesynced, r.State)
	require.Contains(t, r.ErrorMsg, "getRepo")
}

// TestEngineRecoverRepoInvalidCAR covers the path where the response body is
// not a valid CAR.
func TestEngineRecoverRepoInvalidCAR(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte("not a car"))
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e := New(db, fakeClients{base: base}, &memEmitter{}, NewVerifier(nil), 1, m)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateDesynced).Error)

	err = e.recoverRepo(t.Context(), space, "did:plc:alice")
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateDesynced, r.State)
	require.Contains(t, r.ErrorMsg, "parse repo car")
}

type failClients struct{}

func (failClients) ClientForSpace(context.Context, habitat_syntax.SpaceURI) (*http.Client, error) {
	return nil, fmt.Errorf("no client")
}

// mockDir is a test identity.Directory.
type mockDir struct {
	idents map[syntax.DID]*identity.Identity
}

func (m *mockDir) LookupDID(_ context.Context, did syntax.DID) (*identity.Identity, error) {
	id, ok := m.idents[did]
	if !ok {
		return nil, fmt.Errorf("did %s not found", did)
	}
	return id, nil
}
func (m *mockDir) LookupHandle(_ context.Context, _ syntax.Handle) (*identity.Identity, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockDir) Lookup(_ context.Context, _ syntax.AtIdentifier) (*identity.Identity, error) {
	return nil, fmt.Errorf("not implemented")
}
func (m *mockDir) Purge(_ context.Context, _ syntax.AtIdentifier) error { return nil }

// TestVerifierSignerWebAuthor covers the signer path for did:web authors.
func TestVerifierSignerWebAuthor(t *testing.T) {
	t.Parallel()

	priv, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)

	authorDID := syntax.DID("did:web:alice.example.com")
	ident := &identity.Identity{
		DID: authorDID,
		Keys: map[string]identity.VerificationMethod{
			"atproto": {
				Type:               "Multikey",
				PublicKeyMultibase: pub.Multibase(),
			},
		},
	}

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{authorDID: ident}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	var lt spacecommit.LtHash
	_ = spacecommit.SignedCommit{
		Ver:  spacecommit.Version,
		Hash: lt.Sum(),
		Rev:  "3kzl6abcde02k",
	}

	got, err := v.signer(t.Context(), space, authorDID)
	require.NoError(t, err)
	require.Equal(t, pub.Multibase(), got.Multibase())
}

// TestVerifierSignerExternalAuthor covers the signer path for external
// (non-did:web) authors where the host signs the commit.
func TestVerifierSignerExternalAuthor(t *testing.T) {
	t.Parallel()

	priv, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)

	spaceOwner := syntax.DID("did:plc:owner")
	hostDID := syntax.DID("did:web:host.example.com")
	ownerIdent := &identity.Identity{
		DID: spaceOwner,
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://host.example.com"},
		},
	}
	hostIdent := &identity.Identity{
		DID: hostDID,
		Keys: map[string]identity.VerificationMethod{
			"habitat": {
				Type:               "Multikey",
				PublicKeyMultibase: pub.Multibase(),
			},
		},
	}

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{
		spaceOwner: ownerIdent,
		hostDID:    hostIdent,
	}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	got, err := v.signer(t.Context(), space, "did:plc:external")
	require.NoError(t, err)
	require.Equal(t, pub.Multibase(), got.Multibase())
}

// TestVerifierSignerAuthorLookupError covers the error path when author
// DID lookup fails.
func TestVerifierSignerAuthorLookupError(t *testing.T) {
	t.Parallel()

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:web:missing.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lookup author")
}

// TestVerifierSignerAuthorNoKey covers the error path when the author's
// identity has no public key.
func TestVerifierSignerAuthorNoKey(t *testing.T) {
	t.Parallel()

	authorDID := syntax.DID("did:web:alice.example.com")
	ident := &identity.Identity{
		DID:  authorDID,
		Keys: map[string]identity.VerificationMethod{},
	}

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{authorDID: ident}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, authorDID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "author signing key")
}

// TestVerifierSignerExternalOwnerLookupError covers the error path when
// external author's space owner lookup fails.
func TestVerifierSignerExternalOwnerLookupError(t *testing.T) {
	t.Parallel()

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lookup space owner")
}

// TestVerifierSignerExternalNoHabitatService covers the error path when the
// space owner has no habitat service endpoint.
func TestVerifierSignerExternalNoHabitatService(t *testing.T) {
	t.Parallel()

	spaceOwner := syntax.DID("did:plc:owner")
	ownerIdent := &identity.Identity{
		DID:      spaceOwner,
		Services: map[string]identity.ServiceEndpoint{},
	}

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{spaceOwner: ownerIdent}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no habitat service")
}

// TestVerifierSignerExternalHostLookupError covers the error path when the
// host DID lookup fails.
func TestVerifierSignerExternalHostLookupError(t *testing.T) {
	t.Parallel()

	spaceOwner := syntax.DID("did:plc:owner")
	ownerIdent := &identity.Identity{
		DID: spaceOwner,
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://host.example.com"},
		},
	}

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{spaceOwner: ownerIdent}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lookup host")
}

// TestVerifierSignerExternalHostNoKey covers the error path when the host
// has no habitat verification method.
func TestVerifierSignerExternalHostNoKey(t *testing.T) {
	t.Parallel()

	spaceOwner := syntax.DID("did:plc:owner")
	hostDID := syntax.DID("did:web:host.example.com")
	ownerIdent := &identity.Identity{
		DID: spaceOwner,
		Services: map[string]identity.ServiceEndpoint{
			"habitat": {URL: "https://host.example.com"},
		},
	}
	hostIdent := &identity.Identity{
		DID:  hostDID,
		Keys: map[string]identity.VerificationMethod{},
	}

	dir := &mockDir{idents: map[syntax.DID]*identity.Identity{
		spaceOwner: ownerIdent,
		hostDID:    hostIdent,
	}}
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "host signing key")
}

// TestEngineDispatchClaimsPendingAndErrorRepos verifies that dispatch claims
// pending and error repos into syncing state.
func TestEngineDispatchClaimsPendingAndErrorRepos(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")

	require.NoError(t, db.Create(&repo{Space: space, DID: "did:plc:pending1", State: statePending}).Error)
	require.NoError(t, db.Create(&repo{Space: space, DID: "did:plc:error1", State: stateError, ErrorMsg: "old"}).Error)

	e.dispatch(t.Context())

	var pending1, error1 repo
	require.NoError(t, db.First(&pending1, "did = ?", "did:plc:pending1").Error)
	require.Equal(t, stateSyncing, pending1.State)
	require.NoError(t, db.First(&error1, "did = ?", "did:plc:error1").Error)
	require.Equal(t, stateSyncing, error1.State)
}

// TestEngineDispatchClaimsDesyncedRepos verifies that dispatch claims
// desynced repos for full recovery.
func TestEngineDispatchClaimsDesyncedRepos(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")

	require.NoError(t, db.Create(&repo{Space: space, DID: "did:plc:desync1", State: stateDesynced}).Error)
	require.NoError(t, db.Create(&repo{Space: space, DID: "did:plc:desync2", State: stateDesynced}).Error)

	e.dispatch(t.Context())

	var r1, r2 repo
	require.NoError(t, db.First(&r1, "did = ?", "did:plc:desync1").Error)
	require.Equal(t, stateSyncing, r1.State)
	require.NoError(t, db.First(&r2, "did = ?", "did:plc:desync2").Error)
	require.Equal(t, stateSyncing, r2.State)
}

// TestEngineSettleCleanRepo verifies that settle moves a non-dirty repo to
// active.
func TestEngineSettleCleanRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateSyncing).Error)

	var lt spacecommit.LtHash
	err := e.settle(t.Context(), db, space, "did:plc:alice", "3kzl6abcde02k", lt.State())
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateActive, r.State)
	require.Equal(t, syntax.TID("3kzl6abcde02k"), r.Rev)
	require.Equal(t, lt.State(), r.Hash)
}

// TestEngineSettleDirtyRepoAlreadyTested covers the dirty-repo path of settle.
// (the existing TestEngineSettleDirtyRepo also covers this)

// TestEngineScheduleRetryBackoff covers scheduleRetry with multiple retries
// to exercise the backoff cap.
func TestEngineScheduleRetryBackoff(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))

	// First retry
	require.NoError(t, e.scheduleRetry(t.Context(), space, "did:plc:alice", stateError, fmt.Errorf("err1")))
	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, 1, r.RetryCount)
	require.Equal(t, stateError, r.State)
	require.Equal(t, "err1", r.ErrorMsg)

	// Second retry
	require.NoError(t, e.scheduleRetry(t.Context(), space, "did:plc:alice", stateError, fmt.Errorf("err2")))
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, 2, r.RetryCount)
	require.Equal(t, "err2", r.ErrorMsg)

	// Third retry
	require.NoError(t, e.scheduleRetry(t.Context(), space, "did:plc:alice", stateDesynced, fmt.Errorf("err3")))
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, 3, r.RetryCount)
	require.Equal(t, stateDesynced, r.State)
}

// TestEngineBackoff verifies the exponential backoff with cap.
func TestEngineBackoff(t *testing.T) {
	t.Parallel()

	d0 := backoff(0, 60)
	require.GreaterOrEqual(t, d0, 1*time.Minute)
	require.Less(t, d0, 2*time.Minute+1000*time.Millisecond)

	d5 := backoff(5, 60)
	require.GreaterOrEqual(t, d5, 32*time.Minute)
	require.Less(t, d5, 33*time.Minute+1000*time.Millisecond)

	d10 := backoff(10, 60)
	require.GreaterOrEqual(t, d10, 60*time.Minute)
	require.Less(t, d10, 61*time.Minute+1000*time.Millisecond)
}

// TestEngineRunJobDispatchesToRecover covers the recover path of runJob.
func TestEngineRunJobDispatchesToRecover(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	failClient := &failClients{}
	e := New(db, failClient, &memEmitter{}, NewVerifier(nil), 1, m)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))

	j := job{Space: space, DID: "did:plc:alice", Recover: true}
	e.runJob(t.Context(), slog.Default(), j)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateDesynced, r.State)
}

// TestEngineRunJobDispatchesToSync covers the sync path of runJob.
func TestEngineRunJobDispatchesToSync(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Commit: habitat.NetworkHabitatSpaceDefsSignedCommit{Ver: 0},
		})
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	e, _, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	_ = base // used via newTestEngine

	j := job{Space: space, DID: "did:plc:alice", Recover: false}
	e.runJob(t.Context(), slog.Default(), j)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateActive, r.State)
}

// TestEngineRunProcessesPendingRepo covers the full Run lifecycle: a pending
// repo is claimed, synced, and settled active.
func TestEngineRunProcessesPendingRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{Ver: 0}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{
			Commit: commit,
		})
	}))
	t.Cleanup(srv.Close)

	e, _, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	e.Run(ctx)

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, stateActive, r.State)
}

// TestEngineRecoverRepoVerifyError covers the path where the CAR is valid
// but the commit verification fails with a non-ErrInvalidCommit error.
func TestEngineRecoverRepoVerifyError(t *testing.T) {
	t.Parallel()

	carBytes := buildMinimalCAR(t)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(carBytes)
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	// Use a verifier with a mock dir that returns LookupDID errors, causing
	// a transient error (not ErrInvalidCommit) during verification.
	mockD := &mockDir{idents: map[syntax.DID]*identity.Identity{}}
	v := NewVerifier(mockD)
	e := New(db, fakeClients{base: base}, &memEmitter{}, v, 1, m)

	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateDesynced).Error)

	err = e.recoverRepo(t.Context(), space, repoDID)
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateDesynced, r.State)
}

// TestEngineRecoverRepoSuccess covers the happy path of recoverRepo: a valid
// CAR is fetched, the hash matches, records are emitted, and the repo settles active.
func TestEngineRecoverRepoSuccess(t *testing.T) {
	t.Parallel()

	carBytes := buildMinimalCAR(t)

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(carBytes)
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	require.NoError(t, AutoMigrate(db))
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	// nil dir makes Verify use hash-only mode: no signer resolution needed.
	v := NewVerifier(nil)
	e := New(db, fakeClients{base: base}, &memEmitter{}, v, 1, m)

	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateDesynced).Error)

	err = e.recoverRepo(t.Context(), space, repoDID)
	require.NoError(t, err)

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateActive, r.State)
}

// TestEngineNotifyWriteBehindHashSameRev covers the case where the notified
// rev matches but the hash differs.
func TestEngineNotifyWriteBehindHashSameRev(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Updates(map[string]any{"state": stateActive, "rev": "aaa"}).Error)

	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:alice", "", []byte{0x01}))

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, statePending, r.State)
}

// TestEngineNotifyWriteAlreadyCurrent covers the case where the repo is
// already at the notified rev and hash.
func TestEngineNotifyWriteAlreadyCurrent(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("ats://did:plc:owner/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Updates(map[string]any{"state": stateActive, "rev": "aaa"}).Error)

	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:alice", "aaa", nil))

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateActive, r.State)
}
