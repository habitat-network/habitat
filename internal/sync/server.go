package sync

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/habitat-network/habitat/internal/events"
	"github.com/habitat-network/habitat/internal/utils"
)

type Server struct {
	eventStore events.EventStream
}

func NewServer(eventStore events.EventStream) *Server {
	return &Server{eventStore: eventStore}
}

func (s *Server) HandleSubscribeSpaces(w http.ResponseWriter, r *http.Request) {
	cursor := r.URL.Query().Get("cursor")
	var lastSeq uint64
	if cursor != "" {
		parsed, err := strconv.ParseUint(cursor, 10, 64)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse cursor", http.StatusBadRequest)
			return
		}
		lastSeq = parsed
	} else if r.Header.Get("Last-Event-ID") != "" {
		parsed, err := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64)
		if err != nil {
			utils.LogAndHTTPError(r.Context(), w, err, "parse cursor", http.StatusBadRequest)
			return
		}
		lastSeq = parsed
	}

	// sse
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

	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := s.eventStore.Subscribe(r.Context(), lastSeq)

	for {
		select {
		case <-r.Context().Done():
			return
		case event := <-ch:
			eventJSON, err := json.Marshal(event)
			if err != nil {
				slog.ErrorContext(r.Context(), "failed to marshal event", "err", err)
				return
			}
			if _, err = fmt.Fprintf(w, "id: %d\n", event.Seq); err != nil {
				slog.ErrorContext(r.Context(), "failed to write id", "err", err)
				return
			}
			if _, err = fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
				slog.ErrorContext(r.Context(), "failed to write event", "err", err)
				return
			}
			if _, err = fmt.Fprintf(w, "data: %s\n\n", eventJSON); err != nil {
				slog.ErrorContext(r.Context(), "failed to write data", "err", err)
				return
			}
			flusher.Flush()
		}
	}
}
