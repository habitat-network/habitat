package httpx

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func ParseDIDInput(
	ctx context.Context,
	w http.ResponseWriter,
	input string,
	name string,
) (syntax.DID, bool) {
	did, err := syntax.ParseDID(input)
	if err != nil {
		WriteInvalidRequest(ctx, w, "failed to parse "+name, err)
		return "", false
	}
	return did, true
}
