package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

// outboxMsg is the wire format sap's /channel websocket emits, mirroring
// cmd/sap's outboxWireMessage.
type outboxMsg struct {
	ID    uint            `json:"id"`
	URI   string          `json:"uri"`
	Value json.RawMessage `json:"value"`
}

// testCollection is the record collection the workload writes into. It is not
// a registered lexicon; putRecord only validates when asked to.
const testCollection = "network.habitat.test"

// writersPerSpace is how many goroutines write into each repo concurrently
// per wave, so writes to the same repo genuinely contend. Every write to a
// repo serializes through pear's one advisory lock per (space, repo), so this
// (together with hotKeysPerRepo, which adds ops rather than parallelism) sets
// how deep that lock's queue gets: too deep and a repo can still be draining
// writes well after sap opportunistically recovers it mid-flight, leaving
// convergence dependent on a later notifyWrite or crawl pass to close the
// gap rather than on this wave's own writes finishing in bounded time.
const writersPerSpace = 10

// hotKeysPerRepo is how many rkeys, within each repo, every wave's workers
// race to upsert on top of their own unique record. This is what actually
// exercises same-record concurrency: several goroutines writing the identical
// URI at once, rather than each worker owning a disjoint key.
const hotKeysPerRepo = 2

// deleteEvery marks every deleteEvery'th worker's own record for deletion
// after it is created and updated, so each wave also exercises deletions
// racing with everything else rather than only creates and updates.
const deleteEvery = 4

// spacesPerMember is how many spaces each member authors up front.
const spacesPerMember = 3

// tombstone is the JSON value the outbox carries for a deleted record.
const tombstone = "null"

// repoSpace pairs a space with the member whose repo inside it is written to.
type repoSpace struct{ member, space string }

// uriSet is a concurrency-safe set of URIs, backed by a plain mutex+map
// rather than sync.Map so callers never need a type assertion to read it.
type uriSet struct {
	mu  sync.Mutex
	uri map[string]struct{}
}

func newURISet() *uriSet { return &uriSet{uri: map[string]struct{}{}} }

// add reports whether uri was newly added.
func (s *uriSet) add(uri string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.uri[uri]; ok {
		return false
	}
	s.uri[uri] = struct{}{}
	return true
}

func (s *uriSet) each(fn func(uri string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for uri := range s.uri {
		fn(uri)
	}
}

func (s *uriSet) len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.uri)
}

// outboxLog tracks the latest value the outbox has delivered for each URI, so
// deletes can later be confirmed as tombstoned rather than merely "seen".
type outboxLog struct {
	mu    sync.Mutex
	value map[string]json.RawMessage
}

func newOutboxLog() *outboxLog { return &outboxLog{value: map[string]json.RawMessage{}} }

// record stores v as uri's latest value, reporting whether uri is new.
func (l *outboxLog) record(uri string, v json.RawMessage) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, existed := l.value[uri]
	l.value[uri] = v
	return !existed
}

func (l *outboxLog) latest(uri string) (json.RawMessage, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	v, ok := l.value[uri]
	return v, ok
}

// drainOutbox reads sap's outbox websocket until conn is closed, acking every
// message and logging it. Errors end the drain rather than failing the test:
// the connection is closed deliberately during teardown, and the test's real
// assertion is on what arrived.
func drainOutbox(conn *websocket.Conn, log *outboxLog) {
	for {
		var msg outboxMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		log.record(msg.URI, msg.Value)
		if err := conn.WriteJSON(map[string]uint{"id": msg.ID}); err != nil {
			return
		}
	}
}

// convergenceGap holds the URIs still outstanding as of the most recent
// convergence check, so a timeout can report exactly what got stuck instead
// of only a count. It implements fmt.Stringer so require.Eventually's failure
// message reflects the last update even though the message is only rendered
// after the retry loop gives up.
type convergenceGap struct {
	mu             sync.Mutex
	missingCreated []string
	notTombstoned  []string
}

func (g *convergenceGap) update(missingCreated, notTombstoned []string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.missingCreated = missingCreated
	g.notTombstoned = notTombstoned
}

func (g *convergenceGap) String() string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return fmt.Sprintf("%d created records never delivered %v; %d deletes not tombstoned %v",
		len(g.missingCreated), g.missingCreated, len(g.notTombstoned), g.notTombstoned)
}

// registerSapSession tells sap to sync did's repos, authenticating to did's
// habitat host with the JWT-bearer grant rather than an OAuth redirect flow.
func registerSapSession(ctx context.Context, t *testing.T, stack *sapStack, did string) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"did": did})
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, stack.SapInternal+"/session/jwt", bytes.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	respBody, _ := io.ReadAll(resp.Body)
	require.Equal(t, http.StatusOK, resp.StatusCode,
		"register sap session for %s: %s", did, respBody)
}

// writeWave drives one wave of concurrent traffic across every repo: for each
// repo, writersPerSpace goroutines each race a handful of shared "hot" rkeys
// (real same-record contention) and separately create, update, and — for
// every deleteEvery'th worker — delete their own unique record. Each expected
// URI is recorded into created (expected to survive) or deleted (expected to
// be tombstoned), exactly once regardless of how many workers race to write
// it.
func writeWave(
	ctx context.Context,
	t *testing.T,
	pc *pearClient,
	spaces []repoSpace,
	prefix string,
	created, deleted *uriSet,
) {
	t.Helper()
	var wg sync.WaitGroup
	for _, rs := range spaces {
		hotRkeys := make([]string, hotKeysPerRepo)
		for i := range hotRkeys {
			hotRkeys[i] = fmt.Sprintf("%s-hot-%d", prefix, i)
		}

		for w := range writersPerSpace {
			wg.Add(1)
			go func() {
				defer wg.Done()

				// Race a write into a shared hot key alongside the other
				// workers in this repo this wave.
				hotRkey := hotRkeys[w%len(hotRkeys)]
				hotURI, err := pc.putRecord(
					ctx, rs.member, rs.space, testCollection, hotRkey, map[string]any{"w": w})
				if err != nil {
					t.Errorf("putRecord hot %s in %s: %v", hotRkey, rs.space, err)
					return
				}
				created.add(hotURI)

				// This worker's own record: create, update, and — for every
				// deleteEvery'th worker — delete it again.
				rkey := fmt.Sprintf("%s-%d", prefix, w)
				uri, err := pc.putRecord(
					ctx, rs.member, rs.space, testCollection, rkey, map[string]any{"n": w})
				if err != nil {
					t.Errorf("putRecord %s in %s: %v", rkey, rs.space, err)
					return
				}
				if _, err := pc.putRecord(ctx, rs.member, rs.space, testCollection, rkey,
					map[string]any{"n": w, "v": 2}); err != nil {
					t.Errorf("update %s in %s: %v", rkey, rs.space, err)
					return
				}
				if w%deleteEvery == 0 {
					if err := pc.deleteRecord(ctx, rs.member, rs.space, testCollection, rkey); err != nil {
						t.Errorf("delete %s in %s: %v", rkey, rs.space, err)
						return
					}
					deleted.add(uri)
					return
				}
				created.add(uri)
			}()
		}
	}
	wg.Wait()
}

// TestSapSyncsPearRecords is the end-to-end proof that sap converges on
// everything written to pear. It stands up the full stack, bootstraps an org
// with three members each owning several spaces, registers a jwt-bearer sap
// session per member, then writes records concurrently across every repo in
// three waves: one with a healthy notifyWrite path, one with that path taken
// down entirely, and one with it merely slow. Each wave also grows a new
// space per member mid-run and races many workers against a handful of
// shared "hot" records per repo and against deletes of their own records, so
// convergence has to survive real contention on the same record, not just
// disjoint concurrent writes. Every record created must eventually surface on
// sap's outbox websocket with its latest value — including the wave whose
// notifications were never delivered, which only converges if sap repairs
// itself rather than trusting pear's best-effort push — and every deleted
// record must surface as a tombstone (a JSON null value on its URI).
func TestSapSyncsPearRecords(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	stack := startSapStack(ctx, t)
	pc := newPearClient(stack.PearHTTP, stack.PearBaseURL, stack.TestClientID, stack.TestClientKey)

	// Bootstrap: an org with its admin, plus two invited members.
	org, err := pc.createOrg(ctx, "itest-org", "admin")
	require.NoError(t, err)
	members := []string{org.AdminDid}
	for _, handle := range []string{"alice", "bob"} {
		token, err := pc.issueInvite(ctx, org.AdminDid)
		require.NoError(t, err)
		did, err := pc.mintMember(ctx, org.OrgId, token, handle)
		require.NoError(t, err)
		members = append(members, did)
	}

	// Each member authors several spaces, so the workload spans many repos.
	var spaces []repoSpace
	for i, member := range members {
		for j := range spacesPerMember {
			uri, err := pc.createSpace(
				ctx, member, "network.habitat.group", fmt.Sprintf("space-%d-%d", i, j))
			require.NoError(t, err)
			spaces = append(spaces, repoSpace{member: member, space: uri})
		}
	}

	// Register sap sessions only after the spaces exist, so the backfill crawl
	// discovers them and subscribes to their notifications.
	for _, member := range members {
		registerSapSession(ctx, t, stack, member)
	}

	// Drain the outbox for the whole run, starting before any record is
	// written so nothing is missed.
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, stack.SapOutboxWS, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	log := newOutboxLog()
	go drainOutbox(conn, log)

	created := newURISet()
	deleted := newURISet()

	// Wave 1: healthy notify path — the baseline push-driven sync.
	writeWave(ctx, t, pc, spaces, "clean", created, deleted)

	// Grow the space set mid-run: one more space per member, so sap's
	// periodic crawl has to discover new spaces concurrently with recovering
	// from the notify blackout in wave 2, not only at startup.
	for i, member := range members {
		uri, err := pc.createSpace(
			ctx, member, "network.habitat.group", fmt.Sprintf("space-%d-late", i))
		require.NoError(t, err)
		spaces = append(spaces, repoSpace{member: member, space: uri})
	}

	// Wave 2: the notify path is down for the entire wave, so pear's
	// best-effort notifications are lost outright. These records can only
	// converge through sap's own recovery, not through a push it never got.
	require.NoError(t, stack.NotifyProxy.Disable())
	writeWave(ctx, t, pc, spaces, "dark", created, deleted)
	require.NoError(t, stack.NotifyProxy.Enable())

	// Wave 3: notifications are delivered but heavily delayed and reordered,
	// overlapping the recovery of wave 2.
	_, err = stack.NotifyProxy.AddToxic("latency", "latency", "downstream", 1.0,
		toxiclient.Attributes{"latency": 800, "jitter": 400})
	require.NoError(t, err)
	writeWave(ctx, t, pc, spaces, "slow", created, deleted)
	require.NoError(t, stack.NotifyProxy.RemoveToxic("latency"))

	// Convergence of the dark wave waits on sap's repair sweep rather than on
	// a push, so the budget here is generous: observed runs settle in a bit
	// over two minutes. Convergence also requires every deleted record to
	// have arrived as a tombstone, not merely to have been seen once. gap
	// tracks exactly which URIs are still outstanding as of the last check,
	// so a timeout reports what got stuck instead of only a count.
	gap := &convergenceGap{}
	require.Eventually(t, func() bool {
		var missingCreated []string
		created.each(func(uri string) {
			if _, ok := log.latest(uri); !ok {
				missingCreated = append(missingCreated, uri)
			}
		})
		var notTombstoned []string
		deleted.each(func(uri string) {
			v, ok := log.latest(uri)
			if !ok || string(v) != tombstone {
				notTombstoned = append(notTombstoned, uri)
			}
		})
		gap.update(missingCreated, notTombstoned)
		t.Logf("outbox: %d/%d created records missing, %d/%d deletes not tombstoned",
			len(missingCreated), created.len(), len(notTombstoned), deleted.len())
		return len(missingCreated) == 0 && len(notTombstoned) == 0
	}, 5*time.Minute, time.Second, "sap did not converge: %s", gap)

	created.each(func(uri string) {
		v, ok := log.latest(uri)
		require.True(t, ok, "created record never delivered: %v", uri)
		require.NotEqual(t, tombstone, string(v),
			"record delivered as a tombstone but was never deleted: %v", uri)
	})
	deleted.each(func(uri string) {
		v, ok := log.latest(uri)
		require.True(t, ok, "deleted record never delivered: %v", uri)
		require.Equal(t, tombstone, string(v),
			"deleted record was not delivered as a tombstone: %v", uri)
	})
}
