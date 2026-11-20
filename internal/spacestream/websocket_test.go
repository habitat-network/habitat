package spacestream

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/spaces"
	"github.com/habitat-network/habitat/internal/spacescope"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// setupWebSocket serves sub over a WebSocket and dials it. The returned
// channel receives ServeWebSocket's result.
func setupWebSocket(
	t *testing.T,
	log *fakeLog,
	sub Subscription,
) (*websocket.Conn, <-chan error) {
	t.Helper()
	server := NewServer(log, &fakeRoles{readable: map[habitat_syntax.SpaceURI]bool{spaceA: true}},
		NewBroadcaster())
	result := make(chan error, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result <- server.ServeWebSocket(w, r, sub)
	}))
	t.Cleanup(srv.Close)
	conn, resp, err := websocket.DefaultDialer.DialContext(
		t.Context(), "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	t.Cleanup(func() { _ = conn.Close() })
	return conn, result
}

func readWSFrame(t *testing.T, conn *websocket.Conn) frame {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	var f frame
	require.NoError(t, json.Unmarshal(raw, &f))
	return f
}

func TestServeWebSocketStreamsUntilSubscriberCloses(t *testing.T) {
	log := &fakeLog{}
	log.add(spaceA, "1")
	conn, result := setupWebSocket(t, log, Subscription{
		Query:  Query{Cursor: new(spaces.EventSeq(0))},
		Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}},
	})
	require.Equal(t, []string{"commit 1 1"}, summary([]frame{readWSFrame(t, conn)}))

	require.NoError(t, conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
		time.Now().Add(time.Second)))
	require.NoError(t, <-result, "a subscriber hanging up is not an error")
}

func TestServeWebSocketRejectsClientData(t *testing.T) {
	conn, result := setupWebSocket(
		t,
		&fakeLog{},
		Subscription{Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}}},
	)
	require.NoError(
		t,
		conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"options_update"}`)),
	)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err := conn.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.ClosePolicyViolation), err)
	require.NoError(t, <-result)
}

func TestServeWebSocketSendsExpiry(t *testing.T) {
	conn, result := setupWebSocket(t, &fakeLog{}, Subscription{
		Reader:    SpaceCredentialReader{Space: spaceA},
		ExpiresAt: time.Now().Add(100 * time.Millisecond),
	})
	require.Equal(t, []string{"error ExpiredToken"}, summary([]frame{readWSFrame(t, conn)}))
	require.NoError(t, <-result)

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err := conn.ReadMessage()
	require.True(t, websocket.IsCloseError(err, websocket.CloseNormalClosure), err)
}

func TestServeWebSocketRequiresUpgrade(t *testing.T) {
	server := NewServer(&fakeLog{}, &fakeRoles{}, NewBroadcaster())
	w := httptest.NewRecorder()
	err := server.ServeWebSocket(w, httptest.NewRequest(http.MethodGet, "/", http.NoBody),
		Subscription{Reader: UserReader{DID: alice, Scope: spacescope.Unrestricted{}}})
	require.Error(t, err)
	require.Equal(t, http.StatusBadRequest, w.Code)
}
