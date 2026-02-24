package utils

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/rs/zerolog/log"
)

type ErrorMessage struct {
	Error string `json:"error"`
}

// LogAndHTTPError logs the error before sending and HTTP error response to the provided writer.
// It takes in both an error and a debug message for verobosity.
func LogAndHTTPError(w http.ResponseWriter, err error, debug string, code int) {
	if shouldLog(err) {
		log.Error().Err(err).Msg(debug)
	}
	w.WriteHeader(code)
	if err != nil {
		_ = json.NewEncoder(w).Encode(&ErrorMessage{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(w).Encode(&ErrorMessage{Error: "unknown error"})
}

func shouldLog(err error) bool {
	return !errors.Is(err, context.Canceled)
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
