package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/habitat-network/habitat/api/habitat"
	db_testutil "github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/did"
	"github.com/habitat-network/habitat/internal/spacecommit"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

type fakeClients struct{ base *url.URL }

func (f fakeClients) ClientForSpace(
	context.Context,
	habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	return &atclient.APIClient{Client: http.DefaultClient, Host: f.base.String()}, nil
}

// memEmitter collects emitted records in memory.
type memEmitter struct {
	mu      sync.Mutex
	emitted []habitat_syntax.SpaceRecordURI
	values  map[habitat_syntax.SpaceRecordURI][]byte
}

func (e *memEmitter) Emit(
	_ context.Context,
	uri habitat_syntax.SpaceRecordURI,
	value []byte,
) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.emitted = append(e.emitted, uri)
	if e.values == nil {
		e.values = make(map[habitat_syntax.SpaceRecordURI][]byte)
	}
	e.values[uri] = value
	return nil
}

func (e *memEmitter) InTx(*gorm.DB) Emitter { return e }

func newTestEngine(t *testing.T, hostURL string) (*Engine, *memEmitter, *gorm.DB) {
	t.Helper()
	db := db_testutil.NewDB(t)
	base, err := url.Parse(hostURL)
	require.NoError(t, err)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	emitter := &memEmitter{}
	e, err := New(db, fakeClients{base: base}, emitter, NewVerifier(nil), 1, m)
	require.NoError(t, err)
	e.jobs = make(chan job, 100)
	return e, emitter, db
}

// TestEngineSyncRepoVerifiesAndSettles covers the incremental happy path: ops
// fold into the LtHash, the head commit's hash matches, and the repo settles
// active with its rev and hash state persisted.
func TestEngineSyncRepoVerifiesAndSettles(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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
		Hash: atdata.Bytes(lt.Sum()),
		Ikm:  atdata.Bytes{},
		Mac:  atdata.Bytes{},
		Sig:  atdata.Bytes{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.space.listRepoOps", r.URL.Path)
		out := habitat.NetworkHabitatSpaceListRepoOpsOutput{Commit: &commit}
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

// TestEngineSyncRepoSinceAheadMarksDesynced pins that a RevNotFound from the
// host (our rev is ahead of the repo head) desyncs the repo so the dispatcher
// rebuilds it from a full getRepo snapshot, instead of retrying forever as a
// generic error.
func TestEngineSyncRepoSinceAheadMarksDesynced(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NotEmpty(t, r.URL.Query().Get("since"))
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(atclient.ErrorBody{
			Name:    "RevNotFound",
			Message: "since revision is ahead of the repo head",
		})
	}))
	t.Cleanup(srv.Close)

	e, _, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{"state": stateSyncing, "rev": "3lrev"}).Error)

	require.NoError(t, e.syncRepo(t.Context(), space, repoDID))

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, stateDesynced, r.State)
	require.Contains(t, r.ErrorMsg, "ahead of the repo head")
}

// TestEngineNewDefaults tests that New normalizes a non-positive parallelism.
func TestEngineNewDefaults(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e, err := New(db, fakeClients{base: &url.URL{}}, &memEmitter{}, NewVerifier(nil), 0, m)
	require.NoError(t, err)
	require.Equal(t, 5, e.parallelism)
}

// TestEngineWithTx verifies WithTx returns a new engine sharing the same
// channels and notifier but with the provided transaction.
func TestEngineWithTx(t *testing.T) {
	t.Parallel()

	db := db_testutil.NewDB(t)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	orig, err := New(db, fakeClients{base: &url.URL{}}, &memEmitter{}, NewVerifier(nil), 1, m)
	require.NoError(t, err)

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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

// TestEngineScheduleRetry covers the retry backoff path.
func TestEngineScheduleRetry(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Update("state", stateSyncing).Error)

	err := e.scheduleRetry(
		t.Context(),
		space,
		"did:plc:alice",
		stateError,
		fmt.Errorf("test error"),
	)
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	failClient := &failClients{}
	e, err := New(db, failClient, &memEmitter{}, NewVerifier(nil), 1, m)
	require.NoError(t, err)

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e, err := New(db, fakeClients{base: base}, &memEmitter{}, NewVerifier(nil), 1, m)
	require.NoError(t, err)

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	e, err := New(db, fakeClients{base: base}, &memEmitter{}, NewVerifier(nil), 1, m)
	require.NoError(t, err)

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

func (failClients) ClientForSpace(
	context.Context, habitat_syntax.SpaceURI,
) (*atclient.APIClient, error) {
	return nil, fmt.Errorf("no client")
}

// TestVerifierSignerWebAuthor covers the signer path for did:web authors.
func TestVerifierSignerWebAuthor(t *testing.T) {
	t.Parallel()

	priv, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)

	authorDID := syntax.DID("did:web:alice.example.com")
	ident := did.New(authorDID).AtprotoKey(pub.Multibase()).Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ident)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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
	ownerIdent := did.New(spaceOwner).ATProtoSpaceHost("https://host.example.com").Build()
	hostIdent := did.New(hostDID).ATProtoSpaceKey(pub.Multibase()).Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ownerIdent)
	dir.Insert(*hostIdent)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	got, err := v.signer(t.Context(), space, "did:plc:external")
	require.NoError(t, err)
	require.Equal(t, pub.Multibase(), got.Multibase())
}

// TestVerifierSignerExternalAuthorFallsBackToAtproto covers the proposal's
// fallback rule: a host that publishes no "#atproto_space" verification
// method is resolved through its "#atproto" key instead.
func TestVerifierSignerExternalAuthorFallsBackToAtproto(t *testing.T) {
	t.Parallel()

	priv, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)

	spaceOwner := syntax.DID("did:plc:owner")
	hostDID := syntax.DID("did:web:host.example.com")
	ownerIdent := did.New(spaceOwner).ATProtoSpaceHost("https://host.example.com").Build()
	hostIdent := did.New(hostDID).AtprotoKey(pub.Multibase()).Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ownerIdent)
	dir.Insert(*hostIdent)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	got, err := v.signer(t.Context(), space, "did:plc:external")
	require.NoError(t, err)
	require.Equal(t, pub.Multibase(), got.Multibase())
}

// TestVerifierSignerAuthorLookupError covers the error path when author
// DID lookup fails.
func TestVerifierSignerAuthorLookupError(t *testing.T) {
	t.Parallel()

	dir := identity.NewMockDirectory()
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:web:missing.example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lookup author")
}

// TestVerifierSignerAuthorNoKey covers the error path when the author's
// identity has no public key.
func TestVerifierSignerAuthorNoKey(t *testing.T) {
	t.Parallel()

	authorDID := syntax.DID("did:web:alice.example.com")
	ident := did.New(authorDID).Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ident)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, authorDID)
	require.Error(t, err)
	require.Contains(t, err.Error(), "author signing key")
}

// TestVerifierSignerExternalOwnerLookupError covers the error path when
// external author's space owner lookup fails.
func TestVerifierSignerExternalOwnerLookupError(t *testing.T) {
	t.Parallel()

	dir := identity.NewMockDirectory()
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lookup space owner")
}

// TestVerifierSignerExternalNoHabitatService covers the error path when the
// space owner has no atproto_space_host service endpoint.
func TestVerifierSignerExternalNoHabitatService(t *testing.T) {
	t.Parallel()

	spaceOwner := syntax.DID("did:plc:owner")
	ownerIdent := did.New(spaceOwner).Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ownerIdent)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "no atproto_space_host service")
}

// TestVerifierSignerExternalHostLookupError covers the error path when the
// host DID lookup fails.
func TestVerifierSignerExternalHostLookupError(t *testing.T) {
	t.Parallel()

	spaceOwner := syntax.DID("did:plc:owner")
	ownerIdent := did.New(spaceOwner).ATProtoSpaceHost("https://host.example.com").Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ownerIdent)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "lookup host")
}

// TestVerifierSignerExternalHostNoKey covers the error path when the host
// has no atproto_space/atproto verification method.
func TestVerifierSignerExternalHostNoKey(t *testing.T) {
	t.Parallel()

	spaceOwner := syntax.DID("did:plc:owner")
	hostDID := syntax.DID("did:web:host.example.com")
	ownerIdent := did.New(spaceOwner).ATProtoSpaceHost("https://host.example.com").Build()
	hostIdent := did.New(hostDID).Build()

	dir := identity.NewMockDirectory()
	dir.Insert(*ownerIdent)
	dir.Insert(*hostIdent)
	v := NewVerifier(dir)
	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")

	_, err := v.signer(t.Context(), space, "did:plc:external")
	require.Error(t, err)
	require.Contains(t, err.Error(), "host signing key")
}

// TestEngineDispatchClaimsPendingAndErrorRepos verifies that dispatch claims
// pending and error repos into syncing state.
func TestEngineDispatchClaimsPendingAndErrorRepos(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")

	require.NoError(
		t,
		db.Create(&repo{Space: space, DID: "did:plc:pending1", State: statePending}).Error,
	)
	require.NoError(
		t,
		db.Create(
			&repo{Space: space, DID: "did:plc:error1", State: stateError, ErrorMsg: "old"},
		).Error,
	)

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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")

	require.NoError(
		t,
		db.Create(&repo{Space: space, DID: "did:plc:desync1", State: stateDesynced}).Error,
	)
	require.NoError(
		t,
		db.Create(&repo{Space: space, DID: "did:plc:desync2", State: stateDesynced}).Error,
	)

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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))

	// First retry
	require.NoError(
		t,
		e.scheduleRetry(t.Context(), space, "did:plc:alice", stateError, fmt.Errorf("err1")),
	)
	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, 1, r.RetryCount)
	require.Equal(t, stateError, r.State)
	require.Equal(t, "err1", r.ErrorMsg)

	// Second retry
	require.NoError(
		t,
		e.scheduleRetry(t.Context(), space, "did:plc:alice", stateError, fmt.Errorf("err2")),
	)
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, 2, r.RetryCount)
	require.Equal(t, "err2", r.ErrorMsg)

	// Third retry
	require.NoError(
		t,
		e.scheduleRetry(t.Context(), space, "did:plc:alice", stateDesynced, fmt.Errorf("err3")),
	)
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
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	failClient := &failClients{}
	e, err := New(db, failClient, &memEmitter{}, NewVerifier(nil), 1, m)
	require.NoError(t, err)

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{})
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRepoOpsOutput{})
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(carBytes)
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	// Use a verifier with a mock dir that returns LookupDID errors, causing
	// a transient error (not ErrInvalidCommit) during verification.
	mockD := identity.NewMockDirectory()
	v := NewVerifier(mockD)
	e, err := New(db, fakeClients{base: base}, &memEmitter{}, v, 1, m)
	require.NoError(t, err)

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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(carBytes)
	}))
	t.Cleanup(srv.Close)
	base, _ := url.Parse(srv.URL)

	db := db_testutil.NewDB(t)
	m, err := NewMetrics(nil, nil)
	require.NoError(t, err)
	// nil dir makes Verify use hash-only mode: no signer resolution needed.
	v := NewVerifier(nil)
	e, err := New(db, fakeClients{base: base}, &memEmitter{}, v, 1, m)
	require.NoError(t, err)

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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
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

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:alice").
		Updates(map[string]any{"state": stateActive, "rev": "aaa"}).Error)

	require.NoError(t, e.NotifyWrite(t.Context(), space, "did:plc:alice", "aaa", nil))

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateActive, r.State)
}

// TestEngineNotifyWriteConcurrentUnknownRepo verifies that concurrent
// notifications for a repo sap has not seen before all succeed. The insert
// races between the existence check and the create, which on Postgres aborts
// the losing transaction with a primary-key violation unless the conflict is
// handled.
func TestEngineNotifyWriteConcurrentUnknownRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")

	const concurrency = 8
	errs := make(chan error, concurrency)
	start := make(chan struct{})
	for range concurrency {
		go func() {
			<-start
			errs <- e.NotifyWrite(context.Background(), space, "did:plc:racer", "aaa", nil)
		}()
	}
	close(start)
	for range concurrency {
		require.NoError(t, <-errs)
	}

	var count int64
	require.NoError(t, db.Model(&repo{}).Where("did = ?", "did:plc:racer").Count(&count).Error)
	require.Equal(t, int64(1), count)
}

// TestEngineScheduleRetryDesyncRecoversPromptly verifies a repo marked
// desynced is claimable immediately rather than sitting in error backoff.
// Recovery is the protocol's designed self-healing path, not a retry after a
// fault, so the first attempt must not be delayed. A repo that keeps
// desyncing still backs off.
func TestEngineScheduleRetryDesyncRecoversPromptly(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	e, _, db := newTestEngine(t, "http://unused.example")
	require.NoError(t, e.Track(t.Context(), space, "did:plc:alice"))

	require.NoError(t, e.scheduleRetry(
		t.Context(), space, "did:plc:alice", stateDesynced, errors.New("hash mismatch")))

	var r repo
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Equal(t, stateDesynced, r.State)
	require.Equal(t, int64(0), r.RetryAfter, "first recovery must be claimable immediately")

	// A repeat desync is a real problem, so it backs off like any other retry.
	require.NoError(t, e.scheduleRetry(
		t.Context(), space, "did:plc:alice", stateDesynced, errors.New("hash mismatch")))
	require.NoError(t, db.First(&r, "did = ?", "did:plc:alice").Error)
	require.Greater(t, r.RetryAfter, time.Now().Unix())
}

// TestEngineRecoverByDiffRefetchesOnlyChangedRecords verifies the narrow
// recovery path: a repo sap already holds an index for is repaired by
// enumerating paths with values excluded and fetching only the records whose
// CID moved. The unchanged records must not be refetched or re-emitted, so a
// recovery costs the consumer nothing for data it already has.
func TestEngineRecoverByDiffRefetchesOnlyChangedRecords(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	coll := syntax.NSID("network.habitat.test")
	rev := syntax.NewTIDClock(0).Next().String()

	// The host holds three records; sap's index is current for two of them.
	hostCids := map[syntax.RecordKey]string{"k1": "bafyaaa", "k2": "bafybbb", "k3": "bafyccc"}
	var lt spacecommit.LtHash
	for rkey, c := range hostCids {
		lt.Add(spacecommit.RecordElement(coll, rkey, c))
	}

	var fetched sync.Map
	var listCalls, commitCalls atomic.Int64
	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/network.habitat.space.getLatestCommit",
		func(w http.ResponseWriter, _ *http.Request) {
			commitCalls.Add(1)
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetLatestCommitOutput{
				Commit: &habitat.NetworkHabitatSpaceDefsSignedCommit{
					Ver:  int64(spacecommit.Version),
					Rev:  rev,
					Hash: atdata.Bytes(lt.Sum()),
					Ikm:  atdata.Bytes{},
					Mac:  atdata.Bytes{},
					Sig:  atdata.Bytes{},
				},
			})
		})
	mux.HandleFunc("/xrpc/network.habitat.space.listRecords",
		func(w http.ResponseWriter, r *http.Request) {
			listCalls.Add(1)
			require.Equal(t, "true", r.URL.Query().Get("excludeValues"))
			out := habitat.NetworkHabitatSpaceListRecordsOutput{}
			for _, rkey := range []syntax.RecordKey{"k1", "k2", "k3"} {
				out.Records = append(out.Records, habitat.NetworkHabitatSpaceListRecordsRecord{
					Collection: coll.String(), Rkey: rkey.String(), Cid: hostCids[rkey],
				})
			}
			_ = json.NewEncoder(w).Encode(out)
		})
	mux.HandleFunc("/xrpc/network.habitat.space.getRecord",
		func(w http.ResponseWriter, r *http.Request) {
			rkey := r.URL.Query().Get("rkey")
			fetched.Store(rkey, true)
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetRecordOutput{
				Cid: hostCids[syntax.RecordKey(rkey)], Value: map[string]any{"k": rkey},
			})
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e, emitter, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	// k1 is current, k2 is stale, k3 is unknown to sap.
	for rkey, c := range map[syntax.RecordKey]string{"k1": "bafyaaa", "k2": "bafyOLD"} {
		require.NoError(t, indexRecord(t.Context(), db, space, repoDID, coll, rkey, c))
	}

	require.NoError(t, e.recoverRepo(t.Context(), space, repoDID))

	require.Equal(t, int64(1), commitCalls.Load())
	require.Equal(t, int64(1), listCalls.Load())
	_, gotK1 := fetched.Load("k1")
	_, gotK2 := fetched.Load("k2")
	_, gotK3 := fetched.Load("k3")
	require.False(t, gotK1, "unchanged record must not be refetched")
	require.True(t, gotK2, "stale record must be refetched")
	require.True(t, gotK3, "unknown record must be fetched")

	require.Len(t, emitter.emitted, 2, "only changed records are re-emitted")

	var r repo
	require.NoError(t, db.First(&r, "did = ?", repoDID).Error)
	require.Equal(t, stateActive, r.State)
	require.Equal(t, syntax.TID(rev), r.Rev)

	index, err := recordIndex(t.Context(), db, space, repoDID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"network.habitat.test/k1": "bafyaaa",
		"network.habitat.test/k2": "bafybbb",
		"network.habitat.test/k3": "bafyccc",
	}, index)
}

// TestEngineRecoverByDiffEmitsDeleteTombstone pins that narrow recovery
// tombstones a record sap's index holds but the host's current listing no
// longer does, rather than silently dropping it from the index. A record can
// reach this path deleted rather than through the incremental sync path
// (e.g. host notifications were lost and sap only reconciles via recovery),
// so recovery must emit the same null tombstone sync.go does or the delete
// never reaches consumers.
func TestEngineRecoverByDiffEmitsDeleteTombstone(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	coll := syntax.NSID("network.habitat.test")
	rev := syntax.NewTIDClock(0).Next().String()

	// The host now holds only k1; k2 was deleted since sap last saw it.
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement(coll, "k1", "bafyaaa"))
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  rev,
		Hash: atdata.Bytes(lt.Sum()),
		Ikm:  atdata.Bytes{},
		Mac:  atdata.Bytes{},
		Sig:  atdata.Bytes{},
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/network.habitat.space.getLatestCommit",
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceGetLatestCommitOutput{
				Commit: &commit,
			})
		})
	mux.HandleFunc("/xrpc/network.habitat.space.listRecords",
		func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(habitat.NetworkHabitatSpaceListRecordsOutput{
				Records: []habitat.NetworkHabitatSpaceListRecordsRecord{
					{Collection: coll.String(), Rkey: "k1", Cid: "bafyaaa"},
				},
			})
		})
	mux.HandleFunc("/xrpc/network.habitat.space.getRecord",
		func(w http.ResponseWriter, r *http.Request) {
			t.Errorf("unchanged record must not be refetched: %s", r.URL.Query().Get("rkey"))
		})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e, emitter, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	for rkey, c := range map[syntax.RecordKey]string{"k1": "bafyaaa", "k2": "bafybbb"} {
		require.NoError(t, indexRecord(t.Context(), db, space, repoDID, coll, rkey, c))
	}

	require.NoError(t, e.recoverRepo(t.Context(), space, repoDID))

	require.Len(t, emitter.emitted, 1, "only the deleted record is emitted")
	require.Equal(
		t,
		[]byte("null"),
		emitter.values["at://did:plc:owner/space/network.habitat.space/s1/did:plc:alice/network.habitat.test/k2"],
	)

	index, err := recordIndex(t.Context(), db, space, repoDID)
	require.NoError(t, err)
	require.Equal(t, map[string]string{"network.habitat.test/k1": "bafyaaa"}, index)
}

// TestEngineRecoverFromCAREmitsDeleteTombstone pins that full-snapshot
// recovery also tombstones a record sap's index holds but the recovered CAR
// no longer does, not just recoverByDiff's narrow path. recoverRepo reaches
// the CAR path here because the narrow-diff endpoints 404, the same way a
// failed narrow recovery falls back to it in production.
func TestEngineRecoverFromCAREmitsDeleteTombstone(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	car := buildMinimalCAR(t) // holds exactly network.habitat.test/rec1

	mux := http.NewServeMux()
	mux.HandleFunc(
		"/xrpc/network.habitat.space.getRepo",
		func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/vnd.ipld.car")
			_, _ = w.Write(car)
		},
	)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	e, emitter, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	// sap's index holds rec1 (still present in the CAR) and rec2 (deleted
	// since sap last synced).
	require.NoError(t, indexRecord(t.Context(), db, space, repoDID,
		"network.habitat.test", "rec1", "bafyaaa"))
	require.NoError(t, indexRecord(t.Context(), db, space, repoDID,
		"network.habitat.test", "rec2", "bafybbb"))

	require.NoError(t, e.recoverRepo(t.Context(), space, repoDID))

	require.Len(t, emitter.emitted, 2, "the recovered record and the deleted one are both emitted")
	require.Equal(
		t,
		[]byte("null"),
		emitter.values["at://did:plc:owner/space/network.habitat.space/s1/did:plc:alice/network.habitat.test/rec2"],
	)

	index, err := recordIndex(t.Context(), db, space, repoDID)
	require.NoError(t, err)
	_, stillHeld := index["network.habitat.test/rec2"]
	require.False(t, stillHeld, "deleted record must be dropped from the index")
}

// TestEngineRecoverRepoUsesHabitatNsid pins that full recovery calls the
// host's network.habitat.space.getRepo endpoint (com.atproto.space.getRepo
// does not exist on the host). The repo holds no local index, so recoverRepo
// must take the full-CAR path rather than the narrow diff.
func TestEngineRecoverRepoUsesHabitatNsid(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")

	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	e, _, db := newTestEngine(t, srv.URL)
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Update("state", stateDesynced).Error)

	require.NoError(t, e.recoverRepo(t.Context(), space, repoDID))
	require.Equal(t, "/xrpc/network.habitat.space.getRepo", requestedPath)
}

// TestEngineSyncRepoEmitsDeleteTombstone pins that a delete op (cid empty,
// value null) is emitted to the outbox as a JSON null so consumers can remove
// their copy, rather than skipped. The op also carries prev, since the lexicon
// requires it (null only for a create); folding prev out and never folding a
// cid in is what leaves the repo's hash consistent with the host's commit.
func TestEngineSyncRepoEmitsDeleteTombstone(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	clock := syntax.NewTIDClock(0)
	rev1, rev2 := clock.Next().String(), clock.Next().String()

	ops := []habitat.NetworkHabitatSpaceListRepoOpsOpEntry{
		{Rev: rev1, Collection: "network.habitat.test", Rkey: "k1", Cid: "bafyaaa",
			Value: map[string]any{"n": 1}},
		{Rev: rev2, Collection: "network.habitat.test", Rkey: "k1", Prev: "bafyaaa"},
	}
	var lt spacecommit.LtHash
	commit := habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(spacecommit.Version),
		Rev:  rev2,
		Hash: atdata.Bytes(lt.Sum()),
		Ikm:  atdata.Bytes{},
		Mac:  atdata.Bytes{},
		Sig:  atdata.Bytes{},
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		out := habitat.NetworkHabitatSpaceListRepoOpsOutput{Commit: &commit}
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
		Where("space = ? AND did = ?", space, repoDID).
		Update("state", stateSyncing).Error)

	require.NoError(t, e.syncRepo(t.Context(), space, repoDID))

	require.Len(t, emitter.emitted, 2)
	require.Equal(
		t,
		[]byte("null"),
		emitter.values["at://did:plc:owner/space/network.habitat.space/s1/did:plc:alice/network.habitat.test/k1"],
	)
}

// TestEngineCheckRequeuesDriftedRepo pins that a backfill crawl's rev/hash
// comparison requeues an active repo that has drifted from the host (a write
// that slipped past the notifier).
func TestEngineCheckRequeuesDriftedRepo(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	clock := syntax.NewTIDClock(0)
	rev1, rev2 := clock.Next().String(), clock.Next().String()

	e, _, db := newTestEngine(t, "https://example.test")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("network.habitat.test", "k1", "bafyaaa"))
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{
			"state": stateActive,
			"rev":   rev1,
			"hash":  lt.State(),
		}).Error)

	// Host reports a newer rev (a write happened that we missed).
	require.NoError(
		t,
		e.Check(
			t.Context(),
			space,
			repoDID,
			syntax.TID(rev2),
			lt.Sum(),
		),
	)

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, statePending, r.State)
}

// TestEngineCheckLeavesCurrentReposActive pins that an up-to-date repo is not
// requeued by the crawl.
func TestEngineCheckLeavesCurrentReposActive(t *testing.T) {
	t.Parallel()

	space := habitat_syntax.SpaceURI("at://did:plc:owner/space/network.habitat.space/s1")
	repoDID := syntax.DID("did:plc:alice")
	rev := syntax.NewTIDClock(0).Next().String()

	e, _, db := newTestEngine(t, "https://example.test")
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement("network.habitat.test", "k1", "bafyaaa"))
	require.NoError(t, e.Track(t.Context(), space, repoDID))
	require.NoError(t, db.Model(&repo{}).
		Where("space = ? AND did = ?", space, repoDID).
		Updates(map[string]any{"state": stateActive, "rev": rev, "hash": lt.State()}).Error)

	require.NoError(
		t,
		e.Check(
			t.Context(),
			space,
			repoDID,
			syntax.TID(rev),
			lt.Sum(),
		),
	)

	var r repo
	require.NoError(t, db.First(&r, "space = ? AND did = ?", space, repoDID).Error)
	require.Equal(t, stateActive, r.State)
}
