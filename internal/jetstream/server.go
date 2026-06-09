package jetstream

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/bluesky-social/jetstream/pkg/models"
	"github.com/bradenaw/juniper/stream"
	"github.com/habitat-network/habitat/internal/authn"
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
	ctx         context.Context
	us          *updateService
	oauthMethod authn.Method
}

func NewServer(
	ctx context.Context,
	in stream.Stream[models.Event],
	oauthMethod authn.Method,
) *Server {
	return &Server{
		ctx:         ctx,
		us:          NewUpdateService(ctx, in),
		oauthMethod: oauthMethod,
	}
}

func (s *Server) HandleSubscribe(w http.ResponseWriter, r *http.Request) {
	wantedCollections := r.URL.Query()["wantedCollections"]
	var requiredScopes []string
	if len(wantedCollections) > 0 {
		for _, c := range wantedCollections {
			requiredScopes = append(requiredScopes, "org:"+c)
		}
	} else {
		requiredScopes = append(requiredScopes, "org:*")
	}
	if _, ok := s.oauthMethod.Validate(w, r, requiredScopes...); !ok {
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Ensure the response writer supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		utils.LogAndHTTPError(
			r.Context(),
			w,
			fmt.Errorf("streaming unsupported on client"),
			"jetstream: connection open",
			http.StatusBadRequest,
		)
		return
	}

	// Flush headers immediately so the client unblocks before the first event arrives.
	// TODO: idk if this is the right thing to do but in tests httpClient.Do blocks until it gets a status header via
	// directly written or a write, and we don't do writes until after sending an event.
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	wantedDIDs := r.URL.Query()["wantedDids"]

	// TODO: support maxMessageSize and compression

	ch, unsubscribe := s.us.subscribe(r.Context(), wantedCollections, wantedDIDs)
	defer unsubscribe()

	for {
		select {
		case <-ctx.Done():
			// Report length of subscription; some other metrics here
			return
		// case <-s.ctx.Done():
		case ev, ok := <-ch:
			if !ok {
				//  the channel being closed indicates that this subscriber is too slow
				// Can i send a message or something?
				return
			} else {
				data, err := json.Marshal(ev)
				if err != nil {
					return
				}
				_, err = fmt.Fprintf(w, "event: update\ndata: %s\n\n", data)
				if err != nil {
					return
				}
				flusher.Flush()
			}
			// case <- t.C send pings to client
		}
	}
}
