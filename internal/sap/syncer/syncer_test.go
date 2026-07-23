package syncer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

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
	return &http.Client{Transport: rewriteTransport{base: f.base}}, nil
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
