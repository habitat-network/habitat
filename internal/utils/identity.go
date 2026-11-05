package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/bluesky-social/indigo/atproto/identity"
)

// SupportsSpaces reports whether the PDS at pdsEndpoint implements the atproto
// permissioned-data ("spaces") protocol — com.atproto.space — per the
// alpha proposal (github.com/bluesky-social/proposals, 0016-permissioned-data).
// A space authority resolves like any other account (#atproto_pds is its space
// host, #atproto signs its credentials), so the DID document carries no
// spaces-specific advertisement to read. Capability is detected empirically
// instead: probe getSpace with no params, a call every implementation
// recognizes, and only treat it as supported when the response is one only a
// real implementation would give — a successful result, or the XRPC
// "InvalidRequest" a lexicon-validating server returns for the missing
// required "space" param (e.g. `{"error":"InvalidRequest","message":"Invalid
// com.atproto.space.getSpace params: Missing required key \"space\""}`
// from a real spaces-alpha PDS). Any other response — a 404/501, a generic
// router error, or some other error name entirely — is treated as
// unsupported, since it isn't a signal a spaces implementation must produce.
func SupportsSpaces(ctx context.Context, client *http.Client, ident *identity.Identity) bool {
	pdsEndpoint := ident.PDSEndpoint()
	for _, knownSpaceHost := range []string{
		"spaces-alpha.host.bsky.network",
	} {
		if strings.Contains(pdsEndpoint, knownSpaceHost) {
			return true
		}
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		ident.PDSEndpoint()+"/xrpc/com.atproto.space.getSpace",
		http.NoBody,
	)
	if err != nil {
		return false
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true
	}

	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return false
	}
	return body.Error == "InvalidRequest"
}
