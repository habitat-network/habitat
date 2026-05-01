package jetstream

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/xmaps"
)

// Server actually listens to subscribers over HTTP/2 SSE and emits events
// We don't use Websocket because it's more expensive and complex operationally,
// and this usecase doesn't support the client sending messages.
//
// The official jetstream API uses bi-directionality so that the client can update
// what collections / repos / etc. it is subscribed to. This is a simple enough usecase
// that the client can close the connection and re-open it with the new parameters and the
// timestamp cursor from where it left off.

type subscriber struct{}

func (s *subscriber) Wants(ev models.Event) bool {
	return true // TODO: logic for filtering based on desired collections / dids / etc.
}

type Server struct {
	hjs HabitatJetstream
	// For now simple set; on each change iterate through and see who wants it
	// Eventually, maintain an index for easy look-ups on collection --> sub / did --> sub
	subscribers xmaps.Set[subscriber]
}

func (s *Server) newSubscriber(collections, dids []string) chan *models.Event {
	sub, ch := s.hjs.Subscribe()
	// add sub to subscribers
	s.subscribers.Add(sub)
	return ch
}

func (s *Server) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	// Set required SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Ensure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	wantedCollections := r.URL.Query()["wantedCollections"]
	wantedDIDs := r.URL.Query()["wantedDids"]

	// TODO: support maxMessageSize and compression

	ch := s.newSubscriber(wantedCollections, wantedDIDs)
	enc := json.NewEncoder(w)

	for {
		select {
		case <-ctx.Done():
			// Report length of subscription; some other metrics here
			return
		// case <-s.ctx.Done():
		case ev := <-ch:
			// TODO: do i need to check if subscriber channel was closed on the sender side?
			// Receive an event from the hjs service and write it out to
			err := enc.Encode(ev)
			if err != nil {
				// break or whatever
				break
			}
			flusher.Flush()
			// case <- t.C send pings to client
		}
	}
}
