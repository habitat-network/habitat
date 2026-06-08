package jetstream

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/stretchr/testify/require"
)

func setupServerTest(
	t *testing.T,
) (sender *stream.PipeSender[models.Event], server *Server, httpServer *httptest.Server, cancel func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sender, receiver := stream.Pipe[models.Event](1)
	srv := NewServer(ctx, receiver, authn.NewStubAuthnForTest(syntax.DID("did:plc:admin")))
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleSubscribe))
	return sender, srv, ts, func() {
		ts.Close()
		cancel()
	}
}

func sseRequest(t *testing.T, ts *httptest.Server) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	return resp
}

// readSSEEvent reads one SSE event from the stream and returns its type and decoded payload.
func readSSEEvent(t *testing.T, r *bufio.Reader) (eventType string, ev models.Event) {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err)
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			// blank line signals end of event
			return eventType, ev
		}
		if after, ok := strings.CutPrefix(line, "event: "); ok {
			eventType = after
		} else if after, ok := strings.CutPrefix(line, "data: "); ok {
			require.NoError(t, json.Unmarshal([]byte(after), &ev))
		}
	}
}

func TestServerSingleSubscriberHTTP(t *testing.T) {
	sender, _, ts, teardown := setupServerTest(t)
	defer teardown()

	resp := sseRequest(t, ts)
	defer func() {
		_ = resp.Body.Close()
	}()

	events := []models.Event{
		{Did: "did:plc:alpha"},
		{Did: "did:plc:beta"},
		{Did: "did:plc:gamma"},
	}

	reader := bufio.NewReader(resp.Body)
	for _, ev := range events {
		require.NoError(t, sender.Send(context.Background(), ev))
		gotType, got := readSSEEvent(t, reader)
		require.Equal(t, "update", gotType)
		require.Equal(t, ev.Did, got.Did)
	}
}

func TestServerFilterByDIDHTTP(t *testing.T) {
	t.Skip("this should fail until filtering is implemented")

	sender, _, ts, teardown := setupServerTest(t)
	defer teardown()

	req, err := http.NewRequest(http.MethodGet, ts.URL+"?wantedDids=did:plc:target", http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()
	require.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	// Send a non-matching event then a matching one.
	// With filtering implemented, only the matching event should arrive.
	require.NoError(t, sender.Send(context.Background(), models.Event{Did: "did:plc:other"}))
	require.NoError(t, sender.Send(context.Background(), models.Event{Did: "did:plc:target"}))

	_, got := readSSEEvent(t, bufio.NewReader(resp.Body))
	require.Equal(t, "did:plc:target", got.Did)
}

func TestServerMultipleSubscribersHTTP(t *testing.T) {
	sender, _, ts, teardown := setupServerTest(t)
	defer teardown()

	// Establish both connections before sending so both handlers are in the event loop.
	resp1 := sseRequest(t, ts)
	defer func() {
		_ = resp1.Body.Close()
	}()
	resp2 := sseRequest(t, ts)
	defer func() {
		_ = resp2.Body.Close()
	}()

	events := []models.Event{
		{Did: "did:plc:alpha"},
		{Did: "did:plc:beta"},
		{Did: "did:plc:gamma"},
	}

	type result struct {
		eventType string
		ev        models.Event
		err       error
	}

	reader1 := bufio.NewReader(resp1.Body)
	reader2 := bufio.NewReader(resp2.Body)

	for _, ev := range events {
		received := make(chan result, 2)
		go func() {
			gotType, got := readSSEEvent(t, reader1)
			received <- result{gotType, got, nil}
		}()
		go func() {
			gotType, got := readSSEEvent(t, reader2)
			received <- result{gotType, got, nil}
		}()

		require.NoError(t, sender.Send(context.Background(), ev))

		r1 := <-received
		r2 := <-received
		require.NoError(t, r1.err)
		require.NoError(t, r2.err)
		require.Equal(t, "update", r1.eventType)
		require.Equal(t, "update", r2.eventType)
		require.Equal(t, ev.Did, r1.ev.Did)
		require.Equal(t, ev.Did, r2.ev.Did)
	}
}

func TestServerClientDisconnectHTTP(t *testing.T) {
	_, srv, ts, teardown := setupServerTest(t)
	defer teardown()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() {
		_ = resp.Body.Close()
	}()

	srv.us.mu.RLock()
	initialCount := len(srv.us.subscribers)
	srv.us.mu.RUnlock()
	require.Equal(t, 1, initialCount)

	// Cancel the client context to simulate disconnect. The client transport returns
	// immediately on cancel, but the server handler may still be running defer unsubscribe().
	// Use Eventually to wait for the server-side teardown to complete.
	cancel()
	_ = resp.Body.Close()

	require.Eventually(t, func() bool {
		srv.us.mu.RLock()
		defer srv.us.mu.RUnlock()
		return len(srv.us.subscribers) == 0
	}, time.Second, time.Millisecond)
}

func TestServerSlowConsumerRemovedHTTP(t *testing.T) {
	sender, srv, ts, teardown := setupServerTest(t)
	defer teardown()

	resp := sseRequest(t, ts)
	defer func() {
		_ = resp.Body.Close()
	}()

	// Flood events without reading the response body. Once the TCP buffer fills the
	// handler stalls, the subscriber channel fills to capacity, and listenForUpdates
	// closes the channel and removes the subscriber.
	removed := make(chan struct{})
	go func() {
		defer close(removed)
		for {
			srv.us.mu.RLock()
			count := len(srv.us.subscribers)
			srv.us.mu.RUnlock()
			if count == 0 {
				return
			}
			_ = sender.Send(context.Background(), models.Event{Did: "did:plc:flood"})
		}
	}()

	// Wait for listenForUpdates to remove the slow subscriber, then drain the response
	// body to unblock any stalled server writes so the handler can read the closed
	// channel and exit cleanly.
	<-removed
	_, _ = io.ReadAll(resp.Body)

	// TODO: this is just asserting the same as above. it should be a counter from inside server.HandleSubscribe
	require.Eventually(t, func() bool {
		srv.us.mu.RLock()
		defer srv.us.mu.RUnlock()
		return len(srv.us.subscribers) == 0
	}, time.Second, time.Millisecond)
}
