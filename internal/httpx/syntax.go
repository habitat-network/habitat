package httpx

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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

func ParseSpaceURIInput(
	ctx context.Context,
	w http.ResponseWriter,
	input string,
	name string,
) (habitat_syntax.SpaceURI, bool) {
	uri, err := habitat_syntax.ParseSpaceURI(input)
	if err != nil {
		WriteInvalidRequest(ctx, w, "failed to parse "+name, err)
		return "", false
	}
	return uri, true
}

func ParseNSIDInput(
	ctx context.Context,
	w http.ResponseWriter,
	input string,
	name string,
) (syntax.NSID, bool) {
	nsid, err := syntax.ParseNSID(input)
	if err != nil {
		WriteInvalidRequest(ctx, w, "failed to parse "+name, err)
		return "", false
	}
	return nsid, true
}
