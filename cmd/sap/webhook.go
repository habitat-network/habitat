package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
)

const webhookPollLimit = 100

// webhookPayload is the JSON body POSTed to the configured webhook URL for
// each outbox message.
type webhookPayload struct {
	ID    uint            `json:"id"`
	URI   string          `json:"uri"`
	Value json.RawMessage `json:"value"`
}

// webhookConsumer delivers outbox messages to a configured HTTP endpoint in
// delivery order. A failed delivery is retried in place with exponential
// backoff, up to backoffMaxElapsedTime; if it still hasn't succeeded, the
// consumer stops hammering it and waits for the next outbox notification
// (a newly emitted message) before retrying the same message again — so a
// persistently unreachable webhook doesn't busy-loop, but a message is
// still never skipped or dropped.
type webhookConsumer struct {
	sap    *sap.Sap
	url    string
	client *http.Client

	// backoffInitialInterval/MaxInterval/MaxElapsedTime are fields rather
	// than left at the library defaults so tests can shrink them directly
	// on a *webhookConsumer instance, the same pattern used for
	// outboxPingPeriod etc. on *server.
	backoffInitialInterval time.Duration
	backoffMaxInterval     time.Duration
	backoffMaxElapsedTime  time.Duration
}

func newWebhookConsumer(s *sap.Sap, url string) *webhookConsumer {
	return &webhookConsumer{
		sap:                    s,
		url:                    url,
		client:                 httpx.NewClient(),
		backoffInitialInterval: backoff.DefaultInitialInterval,
		backoffMaxInterval:     backoff.DefaultMaxInterval,
		backoffMaxElapsedTime:  backoff.DefaultMaxElapsedTime,
	}
}

// run delivers outbox messages to the webhook URL until ctx is done.
func (c *webhookConsumer) run(ctx context.Context) {
	for {
		msgs, err := c.sap.Outbox().Poll(ctx, webhookPollLimit)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return
			}
			slog.ErrorContext(ctx, "webhook: poll outbox", "err", err)
			return
		}

		delivered := true
		for _, msg := range msgs {
			if err := c.deliver(ctx, msg); err != nil {
				if ctx.Err() != nil {
					return
				}
				// Stop at the first message that didn't get delivered
				// within the elapsed-time budget: preserve delivery order,
				// and don't spin retrying it — wait for a new notification.
				delivered = false
				break
			}
			if err := c.sap.Outbox().Ack(ctx, msg.ID); err != nil {
				slog.ErrorContext(ctx, "webhook: ack outbox message", "id", msg.ID, "err", err)
				return
			}
		}

		if delivered && len(msgs) > 0 {
			continue
		}

		select {
		case <-ctx.Done():
			return
		case <-c.sap.Outbox().Watch():
		}
	}
}

// deliver POSTs msg to the webhook URL, retrying with exponential backoff
// for up to backoffMaxElapsedTime. A returned error means delivery didn't
// succeed within that budget (the caller distinguishes ctx cancellation via
// ctx.Err()) — either way the message stays unacked for the next attempt,
// never silently dropped.
func (c *webhookConsumer) deliver(ctx context.Context, msg outbox.Message) error {
	eb := backoff.NewExponentialBackOff()
	eb.InitialInterval = c.backoffInitialInterval
	eb.MaxInterval = c.backoffMaxInterval

	_, err := backoff.Retry(
		ctx, func() (struct{}, error) {
			return struct{}{}, c.post(ctx, msg)
		},
		backoff.WithBackOff(eb),
		backoff.WithMaxElapsedTime(c.backoffMaxElapsedTime),
		backoff.WithNotify(func(err error, next time.Duration) {
			slog.WarnContext(
				ctx, "webhook: delivery failed, retrying",
				"id", msg.ID, "next", next, "err", err,
			)
		}),
	)
	if err != nil && ctx.Err() == nil {
		slog.WarnContext(
			ctx, "webhook: giving up on delivery for now, will retry on next notification",
			"id", msg.ID, "err", err,
		)
	}
	return err
}

func (c *webhookConsumer) post(ctx context.Context, msg outbox.Message) error {
	body, err := json.Marshal(webhookPayload{
		ID:    msg.ID,
		URI:   msg.URI.String(),
		Value: msg.Value,
	})
	if err != nil {
		return fmt.Errorf("marshal webhook payload: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("do webhook request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned status %d", resp.StatusCode)
	}
	return nil
}
