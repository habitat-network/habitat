package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/habitat-network/habitat/internal/sap"
)

const outboxPollLimit = 100

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
// once acked is it marked processed so [sap.Outbox.Poll] stops redelivering
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

	outbox := s.sap.Outbox()
	watch := outbox.Watch()

	pending := map[uint]sap.OutboxMessage{}
	for {
		if len(pending) == 0 {
			msgs, err := outbox.Poll(ctx, outboxPollLimit)
			if err != nil {
				slog.ErrorContext(ctx, "poll outbox", "err", err)
				return
			}
			for _, msg := range msgs {
				if err := conn.WriteJSON(outboxWireMessage{
					ID:    msg.ID,
					URI:   msg.URI.String(),
					Value: msg.Value,
				}); err != nil {
					slog.InfoContext(ctx, "write outbox message", "err", err)
					return
				}
				pending[msg.ID] = msg
			}
		}

		select {
		case <-ctx.Done():
			return
		case id := <-acks:
			msg, ok := pending[id]
			if !ok {
				continue
			}
			if err := msg.Ack(ctx); err != nil {
				slog.ErrorContext(ctx, "ack outbox message", "id", id, "err", err)
				continue
			}
			delete(pending, id)
		case <-watch:
		}
	}
}
