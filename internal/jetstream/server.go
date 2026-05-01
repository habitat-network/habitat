package jetstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/habitat-network/habitat/internal/utils"
)

// This package provides a service that fulfills a jetstream-like API for a habitat organization.
// Products that integrate with habitat need a method to receive real-time changes that are relevant
// to their application and index / aggregate them however they want.
//
// This has no authorization primitives attached; it sends all updates flowing through the repo.
// This will be added as a follow up.

// The Server handles listening for updates from an ingestion stream and fanning them out to many upstream
// jetstream subscribers over HTTP SSE. We don't use Websocket because it's more expensive and complex operationally,
// and this usecase doesn't support the client sending messages.
//
// The official jetstream API uses bi-directionality so that the client can update
// what collections / repos / etc. it is subscribed to. This is a simple enough usecase
// that the client can close the connection and re-open it with the new parameters and the
// timestamp cursor from where it left off.

type Server struct {
	ctx context.Context
	us  *updateService
}

func NewServer(ctx context.Context, in stream.Stream[models.Event]) *Server {
	return &Server{
		ctx: ctx,
		us:  NewUpdateService(ctx, in),
	}
}

func (s *Server) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	// Set required SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Ensure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.LogAndHTTPError(w, fmt.Errorf("streaming unsupported on client"), "jetstream: connection open", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	wantedCollections := r.URL.Query()["wantedCollections"]
	wantedDIDs := r.URL.Query()["wantedDids"]

	// TODO: support maxMessageSize and compression

	ch := s.us.subscribe(r.Context(), wantedCollections, wantedDIDs)
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
