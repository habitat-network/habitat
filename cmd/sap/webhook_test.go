package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/db/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
	"github.com/habitat-network/habitat/pkg/oauthclient"
	"github.com/habitat-network/habitat/pkg/sap"
	"github.com/habitat-network/habitat/pkg/sap/outbox"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// newWebhookTestSap creates a *sap.Sap backed by a fresh test database, with
// no notify registration (Endpoint left empty) — these tests only exercise
// the outbox-to-webhook delivery path.
func newWebhookTestSap(t *testing.T) (*sap.Sap, *gorm.DB) {
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

	return s, db
}

func TestWebhookConsumer_DeliversAndAcks(t *testing.T) {
	t.Parallel()

	s, db := newWebhookTestSap(t)

	var received atomic.Pointer[webhookPayload]
	webhookServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			var payload webhookPayload
			require.NoError(t, json.Unmarshal(body, &payload))
			received.Store(&payload)
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer webhookServer.Close()

	uri := "at://did:plc:org/space/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
	id := createOutboxRow(t, db, uri, `{"text":"hello"}`)

	consumer := newWebhookConsumer(s, webhookServer.URL)

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go consumer.run(ctx)

	require.Eventually(t, func() bool {
		return received.Load() != nil
	}, 5*time.Second, 10*time.Millisecond, "expected webhook to be called")

	payload := received.Load()
	require.Equal(t, id, payload.ID)
	require.Equal(t, uri, payload.URI)
	require.JSONEq(t, `{"text":"hello"}`, string(payload.Value))

	type localOutboxRow struct {
		ID      uint
		AckedAt *time.Time
	}
	require.Eventually(t, func() bool {
		var row localOutboxRow
		require.NoError(t, db.Table("outbox_messages").Where("id = ?", id).First(&row).Error)
		return row.AckedAt != nil
	}, 5*time.Second, 10*time.Millisecond, "expected message to be acked")
}

func TestWebhookConsumer_RetriesWithBackoffUntilSuccess(t *testing.T) {
	t.Parallel()

	s, db := newWebhookTestSap(t)

	var attempts atomic.Int32
	const failuresBeforeSuccess = 3
	webhookServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			n := attempts.Add(1)
			if n <= failuresBeforeSuccess {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer webhookServer.Close()

	uri := "at://did:plc:org/space/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
	id := createOutboxRow(t, db, uri, `{"text":"hello"}`)

	consumer := newWebhookConsumer(s, webhookServer.URL)
	// Shrink backoff so the test doesn't wait through real exponential delays.
	consumer.backoffInitialInterval = time.Millisecond
	consumer.backoffMaxInterval = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go consumer.run(ctx)

	type localOutboxRow struct {
		ID      uint
		AckedAt *time.Time
	}
	require.Eventually(t, func() bool {
		var row localOutboxRow
		require.NoError(t, db.Table("outbox_messages").Where("id = ?", id).First(&row).Error)
		return row.AckedAt != nil
	}, 5*time.Second, 10*time.Millisecond, "expected message to eventually be acked")

	require.GreaterOrEqual(t, attempts.Load(), int32(failuresBeforeSuccess+1))

	// The message must not have been acked before the server started
	// succeeding.
	require.Eventually(t, func() bool {
		return attempts.Load() > failuresBeforeSuccess
	}, time.Second, time.Millisecond)
}

func TestWebhookConsumer_GivesUpAfterElapsedCapAndRetriesOnNextNotification(t *testing.T) {
	t.Parallel()

	s, db := newWebhookTestSap(t)

	var succeeding atomic.Bool
	var attempts atomic.Int32
	webhookServer := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			attempts.Add(1)
			if succeeding.Load() {
				w.WriteHeader(http.StatusOK)
				return
			}
			w.WriteHeader(http.StatusInternalServerError)
		}),
	)
	defer webhookServer.Close()

	uri := "at://did:plc:org/space/network.habitat.space/my-space/did:plc:member/network.habitat.note/k1"
	id := createOutboxRow(t, db, uri, `{"text":"hello"}`)

	consumer := newWebhookConsumer(s, webhookServer.URL)
	consumer.backoffInitialInterval = time.Millisecond
	consumer.backoffMaxInterval = 2 * time.Millisecond
	consumer.backoffMaxElapsedTime = 20 * time.Millisecond

	started := time.Now()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go consumer.run(ctx)

	// Give the elapsed-time budget (20ms) comfortable room to run out, plus
	// the give-up cycle to complete, before sampling a baseline.
	require.Eventually(t, func() bool {
		return time.Since(started) > 200*time.Millisecond
	}, time.Second, 10*time.Millisecond, "wait for the cap cycle to finish")
	require.Positive(t, attempts.Load(), "expected at least one delivery attempt")

	stalled := attempts.Load()
	require.Never(t, func() bool {
		return attempts.Load() > stalled
	}, 100*time.Millisecond, 5*time.Millisecond, "consumer should stop retrying once the elapsed-time budget runs out")

	type localOutboxRow struct {
		ID      uint
		AckedAt *time.Time
	}
	var row localOutboxRow
	require.NoError(t, db.Table("outbox_messages").Where("id = ?", id).First(&row).Error)
	require.Nil(t, row.AckedAt, "message must not be acked while delivery keeps failing")

	// Let future deliveries succeed, then nudge the idle consumer with a new
	// notification — it should pick the still-unacked message back up.
	succeeding.Store(true)
	store, ok := s.Outbox().(*outbox.Store)
	require.True(t, ok)
	require.NoError(t, store.Emit(
		t.Context(), habitat_syntax.SpaceRecordURI(uri), []byte(`{"text":"world"}`),
	))

	require.Eventually(t, func() bool {
		var row localOutboxRow
		require.NoError(t, db.Table("outbox_messages").Where("id = ?", id).First(&row).Error)
		return row.AckedAt != nil
	}, 5*time.Second, 10*time.Millisecond, "expected original message to eventually be acked after notify")
}
