package jetstream

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/stretchr/testify/require"
)

func setupServerTest(t *testing.T) (*stream.PipeSender[models.Event], *httptest.Server, func()) {
	ctx, cancel := context.WithCancel(context.Background())
	sender, receiver := stream.Pipe[models.Event](1)
	srv := NewServer(ctx, receiver)
	ts := httptest.NewServer(http.HandlerFunc(srv.HandleSubscribe))
	return sender, ts, func() {
		ts.Close()
		cancel()
	}
}

func sseRequest(t *testing.T, ts *httptest.Server) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
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
	sender, ts, teardown := setupServerTest(t)
	defer teardown()

	resp := sseRequest(t, ts)
	defer resp.Body.Close()

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

func TestServerMultipleSubscribersHTTP(t *testing.T) {
	sender, ts, teardown := setupServerTest(t)
	defer teardown()

	// Establish both connections before sending so both handlers are in the event loop.
	resp1 := sseRequest(t, ts)
	defer resp1.Body.Close()
	resp2 := sseRequest(t, ts)
	defer resp2.Body.Close()

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
