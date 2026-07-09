package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/atclient"
)

func WriteJSON(ctx context.Context, w http.ResponseWriter, v any) {
	bytes, err := json.Marshal(v)
	if err != nil {
		slog.ErrorContext(ctx, "marshal json", "err", err)
		WriteError(ctx, w, "marshal json", err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(bytes)
	if err != nil {
		slog.ErrorContext(ctx, "write json", "err", err)
	}
}

func WriteError(ctx context.Context, w http.ResponseWriter, name string, msg string, code int) {
	w.WriteHeader(code)
	WriteJSON(ctx, w, atclient.ErrorBody{Name: name, Message: msg})
}

func WriteInvalidRequest(ctx context.Context, w http.ResponseWriter, msg string, err error) {
	slog.WarnContext(ctx, "bad request", "msg", msg, "err", err)
	WriteError(ctx, w, "InvalidRequest", msg, http.StatusBadRequest)
}

func WriteSpaceNotFound(ctx context.Context, w http.ResponseWriter, err error) {
	slog.WarnContext(ctx, "space not found", "err", err)
	WriteError(ctx, w, "SpaceNotFound", err.Error(), http.StatusBadRequest)
}

func WriteNotSupported(ctx context.Context, w http.ResponseWriter, msg string) {
	slog.WarnContext(ctx, "not supported", "msg", msg)
	WriteError(ctx, w, "NotSupported", msg, http.StatusNotImplemented)
}
