package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"github.com/testcontainers/testcontainers-go/modules/toxiproxy"
	"github.com/testcontainers/testcontainers-go/network"
	toxiproxyclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/docker/docker/api/types/container"
)

// fakePear is an in-memory fake of Pear's sync API.
// It serves the HTTP endpoints and SSE event stream that Spacetap's syncer expects.
type sseConn struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

type fakePear struct {
	mu       sync.Mutex
	server   *http.Server
	sseConns []*sseConn

	spaces   []map[string]interface{}
	records  []map[string]interface{}
	members  []map[string]interface{}
	eventLog []map[string]interface{}
}

func newFakePear() *fakePear {
	return &fakePear{
		sseConns: []*sseConn{},
		spaces:   []map[string]interface{}{},
		records:  []map[string]interface{}{},
		members:  []map[string]interface{}{},
		eventLog: []map[string]interface{}{},
	}
}

func (f *fakePear) listen() (port int) {
	mux := http.NewServeMux()
	mux.HandleFunc("/xrpc/network.habitat.sync.listSpaces", f.handleListSpaces)
	mux.HandleFunc("/xrpc/network.habitat.sync.getSpaceState", f.handleGetSpaceState)
	mux.HandleFunc("/xrpc/network.habitat.sync.listRecords", f.handleListRecords)
	mux.HandleFunc("/xrpc/network.habitat.sync.listRecordChanges", f.handleListRecordChanges)
	mux.HandleFunc("/xrpc/network.habitat.sync.getMemberOplog", f.handleGetMemberOplog)
	mux.HandleFunc("/xrpc/network.habitat.sync.subscribeSpaces", f.handleSubscribeSpaces)

	f.server = &http.Server{Handler: mux}
	listener, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		panic(err)
	}
	port = listener.Addr().(*net.TCPAddr).Port
	go f.server.Serve(listener)
	return port
}

func (f *fakePear) close() {
	if f.server != nil {
		f.server.Close()
	}
}

func (f *fakePear) addSpace(space, spaceType, spaceRev, memberRev string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.spaces = append(f.spaces, map[string]interface{}{
		"space":     space,
		"spaceType": spaceType,
		"spaceRev":  spaceRev,
		"memberRev": memberRev,
	})
}

func (f *fakePear) addRecord(space, repo, collection, rkey, rev string, value map[string]interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, map[string]interface{}{
		"space":      space,
		"repo":       repo,
		"collection": collection,
		"rkey":       rkey,
		"rev":        rev,
		"value":      value,
	})
}

func (f *fakePear) addMember(space, did, access, rev string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.members = append(f.members, map[string]interface{}{
		"space":  space,
		"did":    did,
		"action": "add",
		"access": access,
		"rev":    rev,
	})
}

func (f *fakePear) emitEvent(ev map[string]interface{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.eventLog = append(f.eventLog, ev)
	data, _ := json.Marshal(ev)
	for _, c := range f.sseConns {
		fmt.Fprintf(c.w, "event: message\ndata: %s\n\n", data)
		c.flusher.Flush()
	}
}

func (f *fakePear) handleListSpaces(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	writeJSON(w, http.StatusOK, map[string]interface{}{"spaces": f.spaces})
}

func (f *fakePear) handleGetSpaceState(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	space := r.URL.Query().Get("space")
	var found map[string]interface{}
	for _, sp := range f.spaces {
		if sp["space"] == space {
			found = sp
			break
		}
	}
	if found == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "space not found"})
		return
	}
	writeJSON(w, http.StatusOK, found)
}

func (f *fakePear) handleListRecords(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	space := r.URL.Query().Get("space")
	var filtered []map[string]interface{}
	for _, rec := range f.records {
		if rec["space"] == space {
			filtered = append(filtered, rec)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"records": filtered,
		"cursor":  "",
	})
}

func (f *fakePear) handleListRecordChanges(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	since := r.URL.Query().Get("since")
	changes := []map[string]interface{}{}
	for _, ev := range f.eventLog {
		if since != "" {
			rev, _ := ev["rev"].(string)
			if rev <= since {
				continue
			}
		}
		if ev["type"] == "space_record" {
			changes = append(changes, ev)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"changes": changes,
		"cursor":  "",
	})
}

func (f *fakePear) handleGetMemberOplog(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	since := r.URL.Query().Get("since")
	ops := []map[string]interface{}{}
	for _, ev := range f.eventLog {
		if since != "" {
			rev, _ := ev["rev"].(string)
			if rev <= since {
				continue
			}
		}
		if ev["type"] == "space_member" {
			ops = append(ops, ev)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ops":    ops,
		"cursor": "",
	})
}

func (f *fakePear) handleSubscribeSpaces(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	cursorStr := r.URL.Query().Get("cursor")
	var cursor int64
	if cursorStr != "" {
		var err error
		cursor, err = strconv.ParseInt(cursorStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid cursor", http.StatusBadRequest)
			return
		}
	}

	// Send catchup events from eventLog
	if cursor > 0 {
		f.mu.Lock()
		for _, ev := range f.eventLog {
			seqVal := int64(0)
			if v, ok := ev["seq"].(int64); ok {
				seqVal = v
			} else if v, ok := ev["seq"].(float64); ok {
				seqVal = int64(v)
			}

			if seqVal > cursor {
				data, _ := json.Marshal(ev)
				fmt.Fprintf(w, "event: message\ndata: %s\nid: %d\n\n", data, seqVal)
				cursor = seqVal
			}
		}
		f.mu.Unlock()
		flusher.Flush()
	}


	// Register this connection for live events
	f.mu.Lock()
	f.sseConns = append(f.sseConns, &sseConn{w: w, flusher: flusher})
	f.mu.Unlock()

	defer func() {
		f.mu.Lock()
		for i, c := range f.sseConns {
			if c.w == w {
				f.sseConns = append(f.sseConns[:i], f.sseConns[i+1:]...)
				break
			}
		}
		f.mu.Unlock()
	}()

	<-r.Context().Done()
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func TestSpacetapReconnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()

	// ---- Setup: seed data on fake Pear BEFORE Spacetap starts (initial sync picks it up) ----
	fake := newFakePear()
	space := "at://did:plc:test/org.example.space/test"

	fake.addSpace(space, "org.example.space", "rev001", "rev001")
	fake.addMember(space, "did:plc:test", "admin", "rev001")
	fake.addRecord(space, "did:plc:test", "org.example.note", "init", "rev002", map[string]interface{}{
		"text": "initial record",
	})
	fake.emitEvent(map[string]interface{}{
		"type": "space_record", "space": space, "rev": "rev002",
		"repo": "did:plc:test", "collection": "org.example.note",
		"rkey": "init", "action": "create",
		"record": map[string]interface{}{"text": "initial record"},
	})

	pearPort := fake.listen()
	defer fake.close()

	// ---- Create Docker network ----
	nw, err := network.New(ctx)
	require.NoError(t, err)
	defer nw.Remove(ctx)

	// ---- Start toxiproxy ----
	toxi, err := toxiproxy.Run(ctx, "ghcr.io/shopify/toxiproxy:latest",
		toxiproxy.WithProxy("pear", fmt.Sprintf("host.docker.internal:%d", pearPort)),
		network.WithNetwork([]string{"toxiproxy"}, nw),
		testcontainers.WithHostConfigModifier(func(hc *container.HostConfig) {
			hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
		}),
	)
	require.NoError(t, err)
	defer toxi.Terminate(ctx)

	toxiURI, err := toxi.URI(ctx)
	require.NoError(t, err)
	tc := toxiproxyclient.NewClient(toxiURI)
	proxies, err := tc.Proxies()
	require.NoError(t, err)
	proxy, ok := proxies["pear"]
	require.True(t, ok, "proxy 'pear' should exist")

	_, proxyPort, err := net.SplitHostPort(proxy.Listen)
	require.NoError(t, err)

	// ---- Start Spacetap (initial sync runs during startup) ----
	spacetap, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:       "../cmd/spacetap",
				Dockerfile:    "Dockerfile",
				PrintBuildLog: false,
				Repo:          "spacetap",
				Tag:           "e2e",
				KeepImage:     true,
			},
			Name: "integration-spacetap",
			Env: map[string]string{
				"SPACETAP_PEAR_URL":     fmt.Sprintf("http://toxiproxy:%s", proxyPort),
				"SPACETAP_BIND":         ":8080",
				"SPACETAP_DISABLE_ACKS": "true",
				"SPACETAP_LOG_LEVEL":    "info",
				"SPACETAP_DATABASE_URL": "/tmp/spacetap.db",
			},
			ExposedPorts: []string{"8080/tcp"},
			WaitingFor:   wait.ForHTTP("/health").WithPort("8080/tcp").WithStartupTimeout(60 * time.Second),
			Networks:     []string{nw.Name},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.ExtraHosts = []string{"host.docker.internal:host-gateway"}
			},
		},
		Started: true,
	})
	require.NoError(t, err)
	defer spacetap.Terminate(ctx)

	stPort, err := spacetap.MappedPort(ctx, "8080")
	require.NoError(t, err)
	stHost, err := spacetap.Host(ctx)
	require.NoError(t, err)
	stURL := fmt.Sprintf("http://%s:%s", stHost, stPort.Port())

	// ---- Phase 1: Verify initial sync picked up seed data ----
	t.Log("Phase 1: Verify initial sync")
	var stats map[string]interface{}
	require.Eventually(t, func() bool {
		resp, err := http.Get(stURL + "/stats")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&stats)
		return stats["spaces"] == float64(1) && stats["records"] == float64(1)
	}, 30*time.Second, 500*time.Millisecond, "initial sync should have 1 space and 1 record")
	t.Logf("Stats after startup: %+v", stats)

	// ---- Phase 2: Add more records while connected ----
	t.Log("Phase 2: Adding records while connected")
	fake.addRecord(space, "did:plc:test", "org.example.note", "second", "rev003", map[string]interface{}{
		"text": "second record",
	})
	fake.emitEvent(map[string]interface{}{
		"type": "space_record", "space": space, "rev": "rev003",
		"repo": "did:plc:test", "collection": "org.example.note",
		"rkey": "second", "action": "create",
		"record": map[string]interface{}{"text": "second record"},
	})

	require.Eventually(t, func() bool {
		resp, err := http.Get(stURL + "/stats")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&stats)
		return stats["records"] == float64(2)
	}, 15*time.Second, 500*time.Millisecond, "should have synced 2 records")

	// ---- Phase 3: Disconnect Pear via toxiproxy ----
	t.Log("Phase 3: Disconnect Pear")
	require.NoError(t, proxy.Disable())

	require.Eventually(t, func() bool {
		resp, err := http.Get(stURL + "/health")
		if err != nil {
			return false
		}
		resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, 500*time.Millisecond, "Spacetap should stay healthy after disconnect")

	insp, err := spacetap.Inspect(ctx)
	require.NoError(t, err)
	require.True(t, insp.State.Running, "Spacetap should not crash when Pear disconnects")

	// ---- Phase 4: Create records while disconnected (eventLog only, no HTTP records) ----
	t.Log("Phase 4: Creating records while disconnected")
	fake.mu.Lock()
	fake.eventLog = append(fake.eventLog, map[string]interface{}{
		"type": "space_record", "space": space, "rev": "rev004",
		"repo": "did:plc:test", "collection": "org.example.note",
		"rkey": "third", "action": "create",
		"record": map[string]interface{}{"text": "third record (created during disconnect)"},
	})
	fake.eventLog = append(fake.eventLog, map[string]interface{}{
		"type": "space_record", "space": space, "rev": "rev005",
		"repo": "did:plc:test", "collection": "org.example.note",
		"rkey": "fourth", "action": "create",
		"record": map[string]interface{}{"text": "fourth record (also during disconnect)"},
	})
	fake.mu.Unlock()

	require.Eventually(t, func() bool {
		resp, err := http.Get(stURL + "/stats")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&stats)
		return stats["records"] == float64(2)
	}, 10*time.Second, 500*time.Millisecond, "records count should NOT increase during disconnect")

	// ---- Phase 5: Reconnect Pear ----
	t.Log("Phase 5: Reconnect Pear")
	require.NoError(t, proxy.Enable())

	require.Eventually(t, func() bool {
		resp, err := http.Get(stURL + "/stats")
		if err != nil {
			return false
		}
		defer resp.Body.Close()
		json.NewDecoder(resp.Body).Decode(&stats)
		return stats["spaces"] == float64(1) && stats["records"] == float64(4)
	}, 30*time.Second, 500*time.Millisecond, "should resync all 4 records after reconnect")
	t.Logf("Stats after reconnection: %+v", stats)

	insp, err = spacetap.Inspect(ctx)
	require.NoError(t, err)
	require.True(t, insp.State.Running, "Spacetap should be running after recovery")

	t.Log("=== TestSpacetapReconnection PASSED ===")
}
