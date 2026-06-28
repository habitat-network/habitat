package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/gorilla/websocket"
	"github.com/habitat-network/habitat/internal/sap"
	"github.com/habitat-network/habitat/internal/oauth_client"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func openOutboxTestServer(t *testing.T) (*httptest.Server, sap.Sap, *gorm.DB) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(t.TempDir()+"/test.db"), &gorm.Config{})
	require.NoError(t, err)

	store, err := oauth_client.NewGormStore(db)
	require.NoError(t, err)

	cfg := oauth.NewPublicConfig(
		"https://example.com/client-metadata.json",
		"https://example.com/oauth-callback",
		[]string{"atproto"},
	)
	oauthApp := oauth_client.NewApp(&cfg, store)

	s, err := sap.NewSap(sap.SapConfig{
		PublicDomain: "example.com",
		Secret:       "z42tt1ZWxkfKn5ujwLsELfY7191h4q6UCFjeRGf6tKXaMCnX",
		DB:           db,
		OAuthClient:  oauthApp,
	})
	require.NoError(t, err)

	orgs := sap.NewOrgManager(db, "example.com", "z42tt1ZWxkfKn5ujwLsELfY7191h4q6UCFjeRGf6tKXaMCnX")
	server := NewSapServer(s, oauthApp, cfg, orgs)
	mux := http.NewServeMux()
	mux.HandleFunc("/channel", server.handleOutboxChannel)
	httpServer := httptest.NewServer(mux)
	t.Cleanup(httpServer.Close)

	return httpServer, s, db
}

func createOutboxRow(t *testing.T, db *gorm.DB, uri, value string) uint {
	t.Helper()
	require.NoError(t, db.Table("outboxes").Create(map[string]any{
		"uri":   uri,
		"value": []byte(value),
	}).Error)

	var id uint
	require.NoError(t, db.Table("outboxes").
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
		require.NoError(t, db.Table("outboxes").Where("id = ?", id).First(&row).Error)
		return row.AckedAt != nil
	}, 5*time.Second, 50*time.Millisecond, "expected message to be acked")

	// Once acked, the message must no longer be a candidate for delivery.
	remaining, err := s.Poll(t.Context(), 10)
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
