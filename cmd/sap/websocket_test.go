package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/gorilla/websocket"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// openOutboxTestServer starts a test server for the /channel websocket.
// configure, if non-nil, runs against the *server before it starts serving —
// e.g. to shrink the outbox ping/pong timeouts. Each call gets its own
// *server instance, so tests that configure different timeouts stay safe
// under t.Parallel() (see TestServer_OutboxChannelClosesUnresponsiveConnection).
func openOutboxTestServer(
	t *testing.T,
	configure func(*server),
) (*httptest.Server, *sap.Sap, *gorm.DB) {
	t.Helper()

	db := testutil.NewDB(t)

	store, err := oauthclient.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth.NewClientApp(&cfg, store)

	s, err := sap.New(sap.Config{
		DB:          db,
		OAuthClient: oauthApp,
	})
	require.NoError(t, err)

	server := NewSapServer(s, oauthApp, "https://example.com", ConfiguredClientMetadata{})
	if configure != nil {
		configure(server)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/channel", server.handleOutboxChannel)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	return httpServer, s, db
}

func createOutboxRow(t *testing.T, db *gorm.DB, uri, value string) uint {
	t.Helper()
	require.NoError(t, db.Table("outbox_messages").Create(map[string]any{
		"uri":   uri,
		"value": []byte(value),
	}).Error)

	var id uint
	require.NoError(t, db.Table("outbox_messages").
		Select("id").
		Order("id DESC").
		Limit(1).
		Scan(&id).Error)
	return id
}

func dialOutboxChannel(t *testing.T, httpServer *httptest.Server) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/channel"
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	if resp != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestServer_OutboxChannelDeliversAndAcks(t *testing.T) {
	t.Parallel()

	httpServer, s, db := openOutboxTestServer(t, nil)

	uri := "at://did:plc:org/space/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
	id := createOutboxRow(t, db, uri, `{"text":"hello"}`)

	conn := dialOutboxChannel(t, httpServer)

	var msg outboxWireMessage
	require.NoError(t, conn.ReadJSON(&msg))
	require.Equal(t, id, msg.ID)
	require.Equal(t, uri, msg.URI)
	require.JSONEq(t, `{"text":"hello"}`, string(msg.Value))

	require.NoError(t, conn.WriteJSON(outboxAck{ID: msg.ID}))

	type localOutboxRow struct {
		ID      uint
		AckedAt *time.Time
	}
	require.Eventually(t, func() bool {
		var row localOutboxRow
		require.NoError(t, db.Table("outbox_messages").Where("id = ?", id).First(&row).Error)
		return row.AckedAt != nil
	}, 5*time.Second, 50*time.Millisecond, "expected message to be acked")

	// Once acked, the message must no longer be a candidate for delivery.
	remaining, err := s.Outbox().Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

// TestServer_OutboxChannelClosesUnresponsiveConnection guards against a
// silently half-open /channel connection (e.g. the peer process is
// suspended, or the network path breaks without a clean TCP close):
// unlike a clean close or a client that hangs up mid-read, this leaves the
// TCP connection looking perfectly healthy while application data stops
// flowing in both directions, so the server needs its own liveness check to
// notice and tear the connection down (letting the client's normal
// reconnect-and-repoll path recover the backlog) instead of leaving it
// stuck forever.
func TestServer_OutboxChannelClosesUnresponsiveConnection(t *testing.T) {
	t.Parallel()

	httpServer, _, _ := openOutboxTestServer(t, func(s *server) {
		s.outboxPingPeriod = 10 * time.Millisecond
		s.outboxPongWait = 30 * time.Millisecond
		s.outboxWriteWait = 50 * time.Millisecond
	})
	conn := dialOutboxChannel(t, httpServer)

	// gorilla only answers a Ping automatically while something is actively
	// pumping Read, so a connection with no reader at all would never even
	// see the Ping (indistinguishable from "server never pings"). Installing
	// a handler that observes the Ping but skips the default auto-pong
	// simulates a peer that received it but has otherwise gone silent.
	pinged := make(chan struct{}, 1)
	conn.SetPingHandler(func(string) error {
		select {
		case pinged <- struct{}{}:
		default:
		}
		return nil
	})

	readErr := make(chan error, 1)
	go func() {
		_, _, err := conn.ReadMessage()
		readErr <- err
	}()

	select {
	case <-pinged:
	case <-time.After(2 * time.Second):
		t.Fatal("server never sent a ping")
	}

	select {
	case err := <-readErr:
		require.Error(t, err, "server should close a connection that stops answering pings")
	case <-time.After(2 * time.Second):
		t.Fatal("server never closed the unresponsive connection")
	}
}

func TestServer_OutboxChannelRedeliversUnackedAfterReconnect(t *testing.T) {
	t.Parallel()

	httpServer, _, db := openOutboxTestServer(t, nil)

	uri := "at://did:plc:org/space/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
	id := createOutboxRow(t, db, uri, `{"text":"hello"}`)

	conn := dialOutboxChannel(t, httpServer)
	var msg outboxWireMessage
	require.NoError(t, conn.ReadJSON(&msg))
	require.Equal(t, id, msg.ID)
	require.NoError(t, conn.Close())

	// Reconnecting without ever acking should redeliver the same message.
	conn2 := dialOutboxChannel(t, httpServer)
	var msg2 outboxWireMessage
	require.NoError(t, conn2.ReadJSON(&msg2))
	require.Equal(t, id, msg2.ID)
}
