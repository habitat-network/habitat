package spacestream

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// writeTimeout bounds one frame's write, so a subscriber that stops
	// reading is dropped rather than holding the stream open forever.
	writeTimeout = 10 * time.Second
	// pingInterval is how often the server pings; a subscriber that answers
	// none within pongWait is dropped.
	pingInterval = 30 * time.Second
	pongWait     = 2 * pingInterval
)

var upgrader = websocket.Upgrader{
	// Every auth method pear accepts here is an Authorization header, never a
	// cookie, so a cross-site page can't open a stream with a visitor's
	// credentials. Any origin may connect.
	CheckOrigin: func(*http.Request) bool { return true },
}

// errClientClosed is the cause of a stream ending because its subscriber
// hung up or broke protocol.
var errClientClosed = errors.New("subscriber closed the stream")

// ServeWebSocket upgrades the request to a WebSocket and runs sub over it until
// either end closes. Errors before the upgrade are written as HTTP responses
// by the upgrader. It returns nil when the subscriber hung up, and otherwise
// the error that ended the stream.
func (s *Server) ServeWebSocket(w http.ResponseWriter, r *http.Request, sub Subscription) error {
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return err
	}
	defer func() { _ = ws.Close() }()
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)

	// The stream is server-push only, as in Jetstream v2: reading just
	// handles pongs and the close handshake, and a data frame is a protocol
	// violation that closes the connection.
	_ = ws.SetReadDeadline(time.Now().Add(pongWait))
	ws.SetPongHandler(func(string) error {
		return ws.SetReadDeadline(time.Now().Add(pongWait))
	})
	go func() {
		if _, _, err := ws.NextReader(); err == nil {
			closeWith(ws, websocket.ClosePolicyViolation, "subscribe is server-push only")
		}
		cancel(errClientClosed)
	}()
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := ws.WriteControl(
					websocket.PingMessage, nil, time.Now().Add(writeTimeout),
				); err != nil {
					cancel(errClientClosed)
					return
				}
			}
		}
	}()

	err = s.Run(ctx, &wsConn{ws: ws}, sub)
	if errors.Is(context.Cause(ctx), errClientClosed) {
		return nil
	}
	closeWith(ws, websocket.CloseNormalClosure, "")
	if errors.Is(err, context.Canceled) {
		// The request ended: the server is shutting down.
		return nil
	}
	return err
}

// closeWith starts the close handshake. WriteControl is safe alongside the
// stream's own writes.
func closeWith(ws *websocket.Conn, code int, reason string) {
	_ = ws.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(writeTimeout),
	)
}

// wsConn is a [Conn] over a WebSocket. Only [Server.Run] writes data frames,
// so writes don't need a lock of their own.
type wsConn struct {
	ws *websocket.Conn
}

// WriteFrame implements [Conn].
//
// A write is bounded by writeTimeout rather than ctx: a timed-out write
// leaves the WebSocket unusable, and the stream's last frame (ExpiredToken)
// is written after its context has ended.
func (c *wsConn) WriteFrame(_ context.Context, frame []byte) error {
	if err := c.ws.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return err
	}
	return c.ws.WriteMessage(websocket.TextMessage, frame)
}
