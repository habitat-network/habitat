package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/habitat-network/habitat/internal/utils"
)

func WriteJSON(ctx context.Context, w http.ResponseWriter, v interface{}) {
	bytes, err := json.Marshal(v)
	if err != nil {
		utils.LogAndHTTPError(ctx, w, err, "marshal json", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, err = w.Write(bytes)
	if err != nil {
		slog.ErrorContext(ctx, "write json", "err", err)
	}
}
