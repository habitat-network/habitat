package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
)

const outboxPollLimit = 100

// defaultOutboxPingPeriod/PongWait/WriteWait bound the /channel websocket's
// liveness checks. Without them, a connection that goes silently half-open
// (the peer process is suspended, or a network path breaks without a clean
// TCP close) is indistinguishable from a healthy idle one: nothing here
// ever calls Read or Write again, so the delivery loop just sits there
// forever, and every outbox message emitted afterward queues up undelivered
// until something else (a client reconnect) replaces the connection.
// Sending a ping every ping period and requiring a pong within the pong
// wait turns that silent stall into a detected failure that tears the
// connection down promptly, so the client's existing
// reconnect-and-poll-on-connect path (see docSync.ts's run()) picks the
// backlog back up. Held as fields on *server (defaulted here, overridden
// directly by tests) rather than package vars, so tests exercising short
// timeouts under t.Parallel() don't race each other over shared state.
const (
	defaultOutboxPingPeriod = 30 * time.Second
	defaultOutboxPongWait   = 60 * time.Second
	defaultOutboxWriteWait  = 10 * time.Second
)

var outboxUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// outboxWireMessage is the JSON wire format for a single outbox event sent
// over the channel websocket.
type outboxWireMessage struct {
	ID    uint            `json:"id"`
	URI   string          `json:"uri"`
	Value json.RawMessage `json:"value"`
}

// outboxAck is the JSON wire format a client sends back to acknowledge a
// delivered message.
type outboxAck struct {
	ID uint `json:"id"`
}

// handleOutboxChannel streams outbox events to a connected websocket client
// in delivery order. A message is held until the client acks it by ID; only
// once acked is it marked processed so [sap.Sap.Poll] stops redelivering
// it. Unacked messages (e.g. the client disconnects) are redelivered on the
// next connection.
func (s *server) handleOutboxChannel(w http.ResponseWriter, r *http.Request) {
	conn, err := outboxUpgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.ErrorContext(r.Context(), "upgrade outbox websocket", "err", err)
		return
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	// writeMu serializes every write to conn: gorilla/websocket forbids
	// concurrent writers, and both the ping ticker below and the delivery
	// loop write to this same connection.
	var writeMu sync.Mutex
	writeJSON := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(s.outboxWriteWait))
		return conn.WriteJSON(v)
	}
	ping := func() error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(s.outboxWriteWait))
		return conn.WriteMessage(websocket.PingMessage, nil)
	}

	_ = conn.SetReadDeadline(time.Now().Add(s.outboxPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(s.outboxPongWait))
	})

	acks := make(chan uint)
	go func() {
		defer cancel()
		for {
			var ack outboxAck
			if err := conn.ReadJSON(&ack); err != nil {
				return
			}
			select {
			case acks <- ack.ID:
			case <-ctx.Done():
				return
			}
		}
	}()

	go func() {
		defer cancel()
		ticker := time.NewTicker(s.outboxPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := ping(); err != nil {
					slog.InfoContext(ctx, "ping outbox websocket", "err", err)
					return
				}
			}
		}
	}()

	pending := map[uint]outbox.Message{}
	for {
		// Always poll, not just when pending is empty: gating on an empty
		// pending map means one message this client can never ack (a
		// processing error with no client-side retry) would permanently
		// block discovery of every later, unrelated message too — this
		// client's own processing is idempotent (a Yjs merge, or an
		// already-published republish that's now a no-op), so redelivering
		// an outstanding message alongside newly-discovered ones is a safe
		// retry rather than a problem. Already-pending messages are
		// skipped here to avoid redundant writes on every wake.
		msgs, err := s.sap.Outbox().Poll(ctx, outboxPollLimit)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.ErrorContext(ctx, "poll outbox", "err", err)
			return
		}
		for _, msg := range msgs {
			if _, ok := pending[msg.ID]; ok {
				continue
			}
			if err := writeJSON(outboxWireMessage{
				ID:    msg.ID,
				URI:   msg.URI.String(),
				Value: msg.Value,
			}); err != nil {
				slog.InfoContext(ctx, "write outbox message", "err", err)
				return
			}
			pending[msg.ID] = msg
		}

		select {
		case <-ctx.Done():
			return
		case id := <-acks:
			if _, ok := pending[id]; !ok {
				continue
			}
			if err := s.sap.Outbox().Ack(ctx, id); err != nil {
				slog.ErrorContext(ctx, "ack outbox message", "id", id, "err", err)
				continue
			}
			delete(pending, id)
		case <-s.sap.Outbox().Watch():
		}
	}
}
