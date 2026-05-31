package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
)

type EventPayload struct {
	ID      uint        `json:"id"`
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

type Deliverer interface {
	Deliver(ctx context.Context, event OutboxEvent) error
	Name() string
}

type WSClient struct {
	conn *websocket.Conn
	done chan struct{}
}

type WebSocketHub struct {
	mu      sync.RWMutex
	clients map[*WSClient]bool
	log     zerolog.Logger
}

func NewWebSocketHub(log zerolog.Logger) *WebSocketHub {
	return &WebSocketHub{
		clients: make(map[*WSClient]bool),
		log:     log,
	}
}

func (h *WebSocketHub) Add(client *WSClient) {
	h.mu.Lock()
	h.clients[client] = true
	h.mu.Unlock()
}

func (h *WebSocketHub) Remove(client *WSClient) {
	h.mu.Lock()
	delete(h.clients, client)
	h.mu.Unlock()
}

func (h *WebSocketHub) Broadcast(event OutboxEvent) {
	var payload map[string]interface{}
	json.Unmarshal([]byte(event.EventJSON), &payload)

	msg := EventPayload{
		ID:      event.ID,
		Type:    "event",
		Payload: payload,
	}
	data, _ := json.Marshal(msg)

	var toRemove []*WSClient

	h.mu.RLock()
	for client := range h.clients {
		select {
		case <-client.done:
			toRemove = append(toRemove, client)
			continue
		default:
		}
		if err := client.conn.WriteMessage(websocket.TextMessage, data); err != nil {
			h.log.Warn().Err(err).Msg("broadcast write failed")
			toRemove = append(toRemove, client)
		}
	}
	h.mu.RUnlock()

	for _, c := range toRemove {
		h.Remove(c)
	}
}

type WSDeliverer struct {
	hub *WebSocketHub
	db  *gorm.DB
	log zerolog.Logger
}

func NewWSDeliverer(hub *WebSocketHub, db *gorm.DB, log zerolog.Logger) *WSDeliverer {
	return &WSDeliverer{hub: hub, db: db, log: log}
}

func (d *WSDeliverer) Deliver(ctx context.Context, event OutboxEvent) error {
	d.hub.Broadcast(event)
	return nil
}

func (d *WSDeliverer) Name() string { return "websocket" }

type WebhookDeliverer struct {
	webhookURL string
	httpClient *http.Client
	log        zerolog.Logger
}

func NewWebhookDeliverer(webhookURL string, log zerolog.Logger) *WebhookDeliverer {
	return &WebhookDeliverer{
		webhookURL: webhookURL,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		log:        log,
	}
}

func (d *WebhookDeliverer) Deliver(ctx context.Context, event OutboxEvent) error {
	body := bytes.NewReader([]byte(event.EventJSON))
	req, err := http.NewRequestWithContext(ctx, "POST", d.webhookURL, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("webhook returned status %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func (d *WebhookDeliverer) Name() string { return "webhook" }

type FireAndForgetDeliverer struct {
	log zerolog.Logger
}

func NewFireAndForgetDeliverer(log zerolog.Logger) *FireAndForgetDeliverer {
	return &FireAndForgetDeliverer{log: log}
}

func (d *FireAndForgetDeliverer) Deliver(ctx context.Context, event OutboxEvent) error {
	d.log.Debug().Uint("event_id", event.ID).Msg("fire-and-forget delivery (no ack)")
	return nil
}

func (d *FireAndForgetDeliverer) Name() string { return "fire-and-forget" }

type OutboxWorker struct {
	db      *gorm.DB
	cfg     *Config
	deliver Deliverer
	log     zerolog.Logger
	stop    chan struct{}
}

func NewOutboxWorker(db *gorm.DB, cfg *Config, deliver Deliverer, log zerolog.Logger) *OutboxWorker {
	return &OutboxWorker{
		db: db, cfg: cfg, deliver: deliver, log: log, stop: make(chan struct{}),
	}
}

func (w *OutboxWorker) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stop:
			return
		case <-ticker.C:
			w.processBatch(ctx)
		}
	}
}

func (w *OutboxWorker) processBatch(ctx context.Context) {
	var events []OutboxEvent
	w.db.Where("acked = ?", false).
		Order("id ASC").
		Limit(w.cfg.OutboxParallelism).
		Find(&events)

	for _, ev := range events {
		if err := w.deliver.Deliver(ctx, ev); err != nil {
			w.log.Warn().Err(err).Uint("event_id", ev.ID).Msg("delivery failed")
			w.db.Model(&ev).Update("attempts", ev.Attempts+1)
			if ev.Attempts >= 10 {
				w.log.Error().Uint("event_id", ev.ID).Msg("max delivery attempts reached, dropping")
				w.db.Model(&ev).Update("acked", true)
			}
		} else {
			w.db.Model(&ev).Update("acked", true)
		}
	}
}

func (w *OutboxWorker) Stop() { close(w.stop) }
