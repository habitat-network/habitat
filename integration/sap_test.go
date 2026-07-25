package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
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

// writersPerSpace is how many records each wave writes concurrently into each
// repo, so writes to the same repo genuinely contend.
const writersPerSpace = 10

// repoSpace pairs a space with the member whose repo inside it is written to.
type repoSpace struct{ member, space string }

// drainOutbox reads sap's outbox websocket until conn is closed, acking every
// message and recording the distinct URIs delivered. Errors end the drain
// rather than failing the test: the connection is closed deliberately during
// teardown, and the test's real assertion is on what arrived.
func drainOutbox(conn *websocket.Conn, seen *sync.Map, distinct *atomic.Int64) {
	for {
		var msg outboxMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		if _, loaded := seen.LoadOrStore(msg.URI, true); !loaded {
			distinct.Add(1)
		}
		if err := conn.WriteJSON(map[string]uint{"id": msg.ID}); err != nil {
			return
		}
	}
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
	require.Equal(t, http.StatusOK, resp.StatusCode, "register sap session for %s", did)
}

// writeWave writes one record per (repo, worker) concurrently and then updates
// each one, recording the distinct URIs created. Every wave uses its own rkey
// prefix so waves do not overwrite each other.
func writeWave(
	ctx context.Context,
	t *testing.T,
	pc *pearClient,
	spaces []repoSpace,
	prefix string,
	created *sync.Map,
	expected *atomic.Int64,
) {
	t.Helper()
	var wg sync.WaitGroup
	for _, rs := range spaces {
		for w := range writersPerSpace {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rkey := fmt.Sprintf("%s-%d", prefix, w)
				uri, err := pc.putRecord(
					ctx, rs.member, rs.space, testCollection, rkey, map[string]any{"n": w})
				if err != nil {
					t.Errorf("putRecord %s in %s: %v", rkey, rs.space, err)
					return
				}
				if _, loaded := created.LoadOrStore(uri, true); !loaded {
					expected.Add(1)
				}
				if _, err := pc.putRecord(ctx, rs.member, rs.space, testCollection, rkey,
					map[string]any{"n": w, "v": 2}); err != nil {
					t.Errorf("update %s in %s: %v", rkey, rs.space, err)
				}
			}()
		}
	}
	wg.Wait()
}

// TestSapSyncsPearRecords is the end-to-end proof that sap converges on
// everything written to pear. It stands up the full stack, bootstraps an org
// with three members each owning two spaces, registers a jwt-bearer sap
// session per member, then writes records concurrently across every repo in
// three waves: one with a healthy notifyWrite path, one with that path taken
// down entirely, and one with it merely slow. Every record created must
// eventually surface on sap's outbox websocket — including the wave whose
// notifications were never delivered, which only converges if sap repairs
// itself rather than trusting pear's best-effort push.
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

	// Each member authors two spaces, so the workload spans six repos.
	var spaces []repoSpace
	for i, member := range members {
		for j := range 2 {
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
	seen := &sync.Map{}
	var delivered atomic.Int64
	go drainOutbox(conn, seen, &delivered)

	created := &sync.Map{}
	var expected atomic.Int64

	// Wave 1: healthy notify path — the baseline push-driven sync.
	writeWave(ctx, t, pc, spaces, "clean", created, &expected)

	// Wave 2: the notify path is down for the entire wave, so pear's
	// best-effort notifications are lost outright. These records can only
	// converge through sap's own recovery, not through a push it never got.
	require.NoError(t, stack.NotifyProxy.Disable())
	writeWave(ctx, t, pc, spaces, "dark", created, &expected)
	require.NoError(t, stack.NotifyProxy.Enable())

	// Wave 3: notifications are delivered but heavily delayed and reordered,
	// overlapping the recovery of wave 2.
	_, err = stack.NotifyProxy.AddToxic("latency", "latency", "downstream", 1.0,
		toxiclient.Attributes{"latency": 800, "jitter": 400})
	require.NoError(t, err)
	writeWave(ctx, t, pc, spaces, "slow", created, &expected)
	require.NoError(t, stack.NotifyProxy.RemoveToxic("latency"))

	// Convergence of the dark wave waits on sap's repair sweep rather than on
	// a push, so the budget here is generous: observed runs settle in a bit
	// over two minutes.
	require.Eventually(t, func() bool {
		got, want := delivered.Load(), expected.Load()
		t.Logf("outbox delivered %d / expected %d", got, want)
		return got >= want
	}, 5*time.Minute, time.Second, "sap did not sync all records")

	created.Range(func(uri, _ any) bool {
		_, ok := seen.Load(uri)
		require.True(t, ok, "created record never delivered: %v", uri)
		return true
	})
}
