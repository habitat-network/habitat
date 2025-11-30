package utils

import (
	"net/http"

	"github.com/eagraf/habitat-new/internal/logging"
)

var (
	log = logging.NewLogger()
)

// LogAndHTTPError logs the error before sending and HTTP error response to the provided writer.
// It takes in both an error and a debug message for verobosity.
func LogAndHTTPError(w http.ResponseWriter, err error, debug string, code int) {
	log.Error().Err(err).Msg(debug)
	http.Error(w, err.Error(), code)
}
