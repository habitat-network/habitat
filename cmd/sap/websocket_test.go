package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/gorilla/websocket"
	"github.com/habitat-network/habitat/internal/db/testutil"
	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func openOutboxTestServer(t *testing.T) (*httptest.Server, *sap.Sap, *gorm.DB) {
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

	server := NewSapServer(s, oauthApp, nil, "example.com", nil)
	mux := http.NewServeMux()
	mux.HandleFunc("/channel", server.handleOutboxChannel)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	return httpServer, s, db
}

func createOutboxRow(t *testing.T, db *gorm.DB, uri, value string) uint {
	t.Helper()
	require.NoError(t, db.Table("sap_outbox").Create(map[string]any{
		"uri":   uri,
		"value": []byte(value),
	}).Error)

	var id uint
	require.NoError(t, db.Table("sap_outbox").
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

	httpServer, s, db := openOutboxTestServer(t)

	uri := "ats://did:plc:org/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
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
		require.NoError(t, db.Table("sap_outbox").Where("id = ?", id).First(&row).Error)
		return row.AckedAt != nil
	}, 5*time.Second, 50*time.Millisecond, "expected message to be acked")

	// Once acked, the message must no longer be a candidate for delivery.
	remaining, err := s.Outbox().Poll(t.Context(), 10)
	require.NoError(t, err)
	require.Empty(t, remaining)
}

func TestServer_OutboxChannelRedeliversUnackedAfterReconnect(t *testing.T) {
	t.Parallel()

	httpServer, _, db := openOutboxTestServer(t)

	uri := "ats://did:plc:org/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
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

// TestOutboxChannelDeliversBeyondOnePollBatch verifies the channel keeps
// delivering once the first poll batch is drained. The handler only re-polls
// when nothing is pending, so a client that acks everything must still receive
// more messages than outboxPollLimit.
func TestOutboxChannelDeliversBeyondOnePollBatch(t *testing.T) {
	httpServer, _, db := openOutboxTestServer(t)

	const total = outboxPollLimit*2 + 25
	for i := range total {
		createOutboxRow(t, db,
			fmt.Sprintf("ats://did:plc:o/network.habitat.space/s/did:plc:r/c/rec%d", i),
			`{"n":1}`)
	}

	conn, _, err := websocket.DefaultDialer.Dial(
		strings.Replace(httpServer.URL, "http", "ws", 1)+"/channel", nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	seen := 0
	for seen < total {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Second)))
		var msg outboxWireMessage
		require.NoError(t, conn.ReadJSON(&msg), "stalled after %d/%d messages", seen, total)
		seen++
		require.NoError(t, conn.WriteJSON(outboxAck{ID: msg.ID}))
	}
	require.Equal(t, total, seen)
}

// TestOutboxChannelDeliversMessagesEmittedWhileDraining verifies the channel
// keeps up when records are still being emitted as the client drains, which is
// how a live sync behaves. Delivery must not stall on a poll that happened to
// find the outbox empty.
func TestOutboxChannelDeliversMessagesEmittedWhileDraining(t *testing.T) {
	httpServer, _, db := openOutboxTestServer(t)

	conn, _, err := websocket.DefaultDialer.Dial(
		strings.Replace(httpServer.URL, "http", "ws", 1)+"/channel", nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	const total = outboxPollLimit * 3
	go func() {
		for i := range total {
			createOutboxRow(t, db,
				fmt.Sprintf("ats://did:plc:o/network.habitat.space/s/did:plc:r/c/rec%d", i),
				`{"n":1}`)
		}
	}()

	seen := 0
	for seen < total {
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(15*time.Second)))
		var msg outboxWireMessage
		require.NoError(t, conn.ReadJSON(&msg), "stalled after %d/%d messages", seen, total)
		seen++
		require.NoError(t, conn.WriteJSON(outboxAck{ID: msg.ID}))
	}
}
