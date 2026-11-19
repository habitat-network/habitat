package remotesync

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
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/golang-jwt/jwt/v5"
	"github.com/ipfs/go-cid"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"

	_ "github.com/bluesky-social/indigo/atproto/auth" // registers the ES256K signing method

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/internal/httpsig"
	"github.com/habitat-network/habitat/internal/spacecommit"
	"github.com/habitat-network/habitat/internal/spaces"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

var (
	org      = syntax.DID("did:plc:org")
	member   = syntax.DID("did:plc:member")
	noteNSID = syntax.NSID("network.habitat.note")
)

// fakeOp is one entry of the fake PDS's oplog.
type fakeOp struct {
	rev   syntax.TID
	rkey  syntax.RecordKey
	cid   string
	prev  string
	value map[string]any
	// badValue serves a value that doesn't match cid.
	badValue bool
}

// fakePDS is a member's PDS serving com.atproto.space.listRepoOps for one
// space repo, checking the space credential and request signature the way a
// repo host does and authenticating its commit with a MAC.
type fakePDS struct {
	t        *testing.T
	space    habitat_syntax.SpaceURI
	orgKey   atcrypto.PublicKey
	kem      *atcrypto.PrivateKeyP256
	clock    *syntax.TIDClock
	pageSize int

	mu      sync.Mutex
	ops     []fakeOp
	current map[syntax.RecordKey]string
	// badMAC serves a commit MAC under the wrong secret.
	badMAC bool
}

func newFakePDS(t *testing.T, space habitat_syntax.SpaceURI, orgKey atcrypto.PublicKey) *fakePDS {
	kem, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return &fakePDS{
		t:        t,
		space:    space,
		orgKey:   orgKey,
		kem:      kem,
		clock:    syntax.NewTIDClock(0),
		pageSize: 2,
		current:  map[syntax.RecordKey]string{},
	}
}

func recordCID(t *testing.T, value map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	require.NoError(t, err)
	data, err := atdata.UnmarshalJSON(raw)
	require.NoError(t, err)
	encoded, err := atdata.MarshalCBOR(data)
	require.NoError(t, err)
	c, err := cid.NewPrefixV1(cid.DagCBOR, multihash.SHA2_256).Sum(encoded)
	require.NoError(t, err)
	return c.String()
}

// put writes a record, or deletes it when text is empty, and returns the rev.
func (p *fakePDS) put(rkey syntax.RecordKey, text string) syntax.TID {
	p.mu.Lock()
	defer p.mu.Unlock()
	op := fakeOp{rev: p.clock.Next(), rkey: rkey, prev: p.current[rkey]}
	if text != "" {
		op.value = map[string]any{"$type": noteNSID.String(), "text": text}
		op.cid = recordCID(p.t, op.value)
		p.current[rkey] = op.cid
	} else {
		delete(p.current, rkey)
	}
	p.ops = append(p.ops, op)
	return op.rev
}

func (p *fakePDS) head() (syntax.TID, []byte) {
	var lt spacecommit.LtHash
	for rkey, c := range p.current {
		lt.Add(spacecommit.RecordElement(noteNSID, rkey, c))
	}
	return p.ops[len(p.ops)-1].rev, lt.Sum()
}

func (p *fakePDS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t := p.t
	require.Equal(t, "/xrpc/com.atproto.space.listRepoOps", r.URL.Path)
	// The credential must be the org's, for this space, bound to the key
	// that signed the request.
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), httpsig.CredentialScheme+" ")
	require.True(t, ok)
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(token, claims, func(*jwt.Token) (any, error) {
		return p.orgKey, nil
	})
	require.NoError(t, err)
	require.Equal(t, org.String(), claims["iss"])
	require.Equal(t, p.space.String(), claims["sub"])
	cnf, ok := claims["cnf"].(map[string]any)
	require.True(t, ok)
	kid, ok := cnf["kid"].(string)
	require.True(t, ok)
	require.NoError(t, httpsig.VerifyBound(r, kid, "authorization", "atproto-space-audience"))
	require.Equal(t, member.String(), r.Header.Get(httpsig.AudienceHeader))
	require.Equal(t, p.space.String(), r.URL.Query().Get("space"))
	require.Equal(t, member.String(), r.URL.Query().Get("repo"))

	keyShare, requested, err := spacecommit.ParseWantMAC(r.Header, spacecommit.CommitMACType)
	require.NoError(t, err)
	require.True(t, requested)
	secret, err := spacecommit.Decapsulate(p.kem, keyShare)
	require.NoError(t, err)

	p.mu.Lock()
	defer p.mu.Unlock()
	since := syntax.TID(r.URL.Query().Get("since"))
	var page []comatproto.ComAtprotoSpaceListRepoOpsOpEntry
	rest := 0
	for _, op := range p.ops {
		if op.rev <= since {
			continue
		}
		if len(page) == p.pageSize {
			rest++
			continue
		}
		entry := comatproto.ComAtprotoSpaceListRepoOpsOpEntry{
			Rev:        op.rev.String(),
			Collection: noteNSID.String(),
			Rkey:       op.rkey.String(),
			Cid:        op.cid,
			Prev:       op.prev,
		}
		if op.value != nil {
			entry.Value = op.value
			if op.badValue {
				entry.Value = map[string]any{"$type": noteNSID.String(), "text": "forged"}
			}
		}
		page = append(page, entry)
	}
	out := comatproto.ComAtprotoSpaceListRepoOpsOutput{Ops: page}
	if len(page) > 0 {
		out.Cursor = page[len(page)-1].Rev
	}
	if rest == 0 && len(p.ops) > 0 {
		rev, data := p.head()
		commit := spacecommit.New(p.space, member, rev.String(), data)
		macSecret := secret
		if p.badMAC {
			macSecret = []byte("not the secret")
		}
		require.NoError(t, spacecommit.SetMAC(w.Header(), spacecommit.CommitMACType,
			commit.MAC(macSecret)))
		out.Commit = new(commit.ToComAtprotoXRPC())
	}
	w.Header().Set("Content-Type", "application/json")
	require.NoError(t, json.NewEncoder(w).Encode(out))
}

// ownerKeys is the org's key, the only space owner hosted here.
type ownerKeys struct {
	key atcrypto.PrivateKey
}

func (k ownerKeys) PrivateKeyForDID(
	_ context.Context,
	did syntax.DID,
) (atcrypto.PrivateKey, error) {
	if did != org {
		return nil, identity.ErrDIDNotFound
	}
	return k.key, nil
}

// countingNotifier counts the writes it is told of.
type countingNotifier struct {
	mu     sync.Mutex
	writes int
}

func (n *countingNotifier) NotifyWrite(
	context.Context,
	habitat_syntax.SpaceURI,
	syntax.DID,
	syntax.TID,
	[]byte,
	syntax.TID,
	syntax.TID,
) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.writes++
}

func (n *countingNotifier) NotifySpaceDeleted(context.Context, habitat_syntax.SpaceURI) {}

type testEnv struct {
	store    spaces.Store
	pds      *fakePDS
	syncer   *Syncer
	notifier *countingNotifier
	space    habitat_syntax.SpaceURI
}

// setupSyncer returns a space owned by org, with member as an external writer
// whose PDS is a fakePDS, and a Syncer for it.
func setupSyncer(t *testing.T) (*testEnv, func()) {
	t.Helper()
	store := spaces_testutil.NewTestStore(t)
	space, err := store.CreateSpace(t.Context(), org, "network.habitat.group", "external")
	require.NoError(t, err)

	orgKey, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	orgPub, err := orgKey.PublicKey()
	require.NoError(t, err)
	pds := newFakePDS(t, space, orgPub)
	server := httptest.NewServer(pds)

	kemPub, err := pds.kem.PublicKey()
	require.NoError(t, err)
	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: member,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_pds": {Type: "AtprotoPersonalDataServer", URL: server.URL},
		},
		Keys: map[string]identity.VerificationMethod{
			spacecommit.KEMFragment: {Type: "Multikey", PublicKeyMultibase: kemPub.Multibase()},
		},
	})
	hostKey, err := atcrypto.GeneratePrivateKeyK256()
	require.NoError(t, err)
	notifier := &countingNotifier{}
	syncer, err := New(store, dir, ownerKeys{key: orgKey}, hostKey, server.Client(), notifier)
	require.NoError(t, err)
	return &testEnv{
		store: store, pds: pds, syncer: syncer, notifier: notifier, space: space,
	}, server.Close
}

// notifyWrite reports the member's PDS head to the space host, as the PDS's
// notifyWrite does.
func (e *testEnv) notifyWrite(t *testing.T) {
	t.Helper()
	e.pds.mu.Lock()
	rev, data := e.pds.head()
	e.pds.mu.Unlock()
	require.NoError(t, e.store.RegisterRemoteWrite(t.Context(), e.space, member, rev, data))
}

// commits sequences the log and summarizes the member's commits in it as
// "<rkey> <op> <text>".
func (e *testEnv) commits(t *testing.T) []string {
	t.Helper()
	_, err := e.store.SequenceEvents(t.Context())
	require.NoError(t, err)
	events, err := e.store.ListEvents(t.Context(), spaces.EventFilter{
		Repos: []syntax.DID{member},
	}, 0, 1000)
	require.NoError(t, err)
	var out []string
	for _, event := range events {
		commit, ok := event.(spaces.CommitEvent)
		require.True(t, ok)
		text, _ := commit.Value["text"].(string)
		out = append(out, commit.Rkey.String()+" "+string(commit.Operation)+" "+text)
	}
	return out
}

func TestSyncerFetchesExternalWriterRecords(t *testing.T) {
	env, teardown := setupSyncer(t)
	defer teardown()
	ctx := t.Context()

	env.pds.put("a", "one")
	env.pds.put("b", "two")
	env.pds.put("c", "three")
	env.notifyWrite(t)
	remote, err := env.store.GetRemoteRepo(ctx, env.space, member)
	require.NoError(t, err)
	require.True(t, remote.Behind())

	// The first fetch reads the whole oplog, across pages.
	require.NoError(t, env.syncer.Sync(ctx, env.space, member))
	require.Equal(t, []string{"a create one", "b create two", "c create three"}, env.commits(t))
	require.Equal(t, 1, env.notifier.writes)
	remote, err = env.store.GetRemoteRepo(ctx, env.space, member)
	require.NoError(t, err)
	require.False(t, remote.Behind())
	behind, err := env.store.ListRemoteReposBehind(ctx)
	require.NoError(t, err)
	require.Empty(t, behind)

	// Up to date: nothing is fetched.
	require.NoError(t, env.syncer.Sync(ctx, env.space, member))
	require.Equal(t, 1, env.notifier.writes)

	// Later writes are fetched from where the last fetch ended.
	env.pds.put("a", "one, edited")
	env.pds.put("b", "")
	env.notifyWrite(t)
	require.NoError(t, env.syncer.Sync(ctx, env.space, member))
	require.Equal(t, []string{"c create three", "a update one, edited", "b delete "},
		env.commits(t))
}

func TestSyncerRebuildsWhenOpsDontAddUp(t *testing.T) {
	env, teardown := setupSyncer(t)
	defer teardown()
	ctx := t.Context()

	env.pds.put("a", "one")
	env.notifyWrite(t)
	// A stored position that doesn't lead to the repo's head: a record the
	// repo doesn't hold, folded into the stored state.
	ghost := map[string]any{"$type": noteNSID.String(), "text": "ghost"}
	var lt spacecommit.LtHash
	lt.Add(spacecommit.RecordElement(noteNSID, "ghost", recordCID(t, ghost)))
	raw, err := json.Marshal(ghost)
	require.NoError(t, err)
	data, err := atdata.UnmarshalJSON(raw)
	require.NoError(t, err)
	encoded, err := atdata.MarshalCBOR(data)
	require.NoError(t, err)
	require.NoError(t, env.store.ApplyRemoteSync(ctx, env.space, member, spaces.RemoteSync{
		Ops: []spaces.RemoteOp{{
			Rev: "2222222222222", Collection: noteNSID, Rkey: "ghost",
			Cid: recordCID(t, ghost), Value: encoded,
		}},
		Rev:   "2222222222222",
		State: lt,
	}))

	// The incremental fetch doesn't match the commit, so the repo is read
	// again from the start, and the record it doesn't hold is deleted.
	require.NoError(t, env.syncer.Sync(ctx, env.space, member))
	require.ElementsMatch(t, []string{"a create one", "ghost delete "}, env.commits(t))
}

func TestSyncerRejectsUnverifiedOps(t *testing.T) {
	tests := []struct {
		name   string
		tamper func(p *fakePDS)
	}{
		{
			name:   "value that doesn't match its cid",
			tamper: func(p *fakePDS) { p.ops[0].badValue = true },
		},
		{
			name:   "commit with a bad mac",
			tamper: func(p *fakePDS) { p.badMAC = true },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env, teardown := setupSyncer(t)
			defer teardown()
			env.pds.put("a", "one")
			env.notifyWrite(t)
			tt.tamper(env.pds)
			require.Error(t, env.syncer.Sync(t.Context(), env.space, member))
			require.Empty(t, env.commits(t), "nothing unverified is stored")
			remote, err := env.store.GetRemoteRepo(t.Context(), env.space, member)
			require.NoError(t, err)
			require.True(t, remote.Behind(), "the repo is left for the next sweep")
		})
	}
}

func TestSyncerIgnoresLocalWriters(t *testing.T) {
	env, teardown := setupSyncer(t)
	defer teardown()
	_, _, err := env.store.PutRecord(t.Context(), env.space, member, noteNSID, "a",
		spaces_testutil.MustMarshalRecord(t, map[string]any{"text": "local"}))
	require.NoError(t, err)
	require.NoError(t, env.syncer.Sync(t.Context(), env.space, member))
	require.Zero(t, env.notifier.writes)
}

func TestSyncerRunSweepsAndFollowsQueue(t *testing.T) {
	env, teardown := setupSyncer(t)
	defer teardown()
	env.pds.put("a", "one")
	env.notifyWrite(t)

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- env.syncer.Run(ctx) }()
	// The sweep at start finds the repo behind.
	require.Eventually(t, func() bool { return len(env.commits(t)) == 1 },
		waitFor, tick)

	env.pds.put("b", "two")
	env.notifyWrite(t)
	env.syncer.Queue(env.space, member)
	require.Eventually(t, func() bool { return len(env.commits(t)) == 2 },
		waitFor, tick)
	cancel()
	require.ErrorIs(t, <-done, context.Canceled)
}

// Run's goroutine talks to a real HTTP server, so it can't run in a synctest
// bubble; these bound how long the test waits for it.
const (
	waitFor = 5 * time.Second
	tick    = 10 * time.Millisecond
)
