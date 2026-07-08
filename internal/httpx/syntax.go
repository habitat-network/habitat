package httpx

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/utils"
)

func ParseDIDInput(
	ctx context.Context,
	w http.ResponseWriter,
	input string,
	name string,
) (syntax.DID, bool) {
	did, err := syntax.ParseDID(input)
	if err != nil {
		utils.WriteHTTPError(w, err, http.StatusBadRequest)
		return "", false
	}
	return did, true
}
