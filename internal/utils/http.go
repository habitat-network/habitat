package utils

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	habitat_err "github.com/habitat-network/habitat/internal/error"
)

type ErrorMessage struct {
	Error string `json:"error"`
}

// LogAndHTTPError logs the error with the given context before sending and HTTP error response to the provided writer.
// It takes in both an error and a debug message for verobosity.
func LogAndHTTPError(
	ctx context.Context,
	w http.ResponseWriter,
	err error,
	debug string,
	code int,
) {
	if ShouldLog(err) {
		slog.ErrorContext(ctx, "handler error", "err", err, "debug", debug)
	} else {
		slog.WarnContext(ctx, "handler error", "err", err, "debug", debug)
	}
	w.WriteHeader(code)
	if err != nil {
		_ = json.NewEncoder(w).Encode(&ErrorMessage{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(&ErrorMessage{Error: "unknown error"})
}

func ShouldLog(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, habitat_err.ErrUnauthorized)
}

// LogAndHTTPError logs the error before sending and HTTP error response to the provided writer.
// It takes in both an error and a debug message for verobosity.
func WriteHTTPError(w http.ResponseWriter, err error, code int) {
	w.WriteHeader(code)
	if err != nil {
		_ = json.NewEncoder(w).Encode(&ErrorMessage{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(&ErrorMessage{Error: "unknown error"})
}
