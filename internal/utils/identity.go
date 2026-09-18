package utils

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
)

func SpaceHostEndpoint(ident *identity.Identity) string {
	if spaceHost := ident.GetServiceEndpoint("atproto_space_host"); spaceHost != "" {
		return spaceHost
	}
	return ident.PDSEndpoint()
}

// SupportsSpaces reports whether the PDS at pdsEndpoint implements the atproto
// permissioned-data ("spaces") protocol — com.atproto.simplespace — per the
// alpha proposal (github.com/bluesky-social/proposals, 0016-permissioned-data).
// An identity is not required to advertise a dedicated #atproto_space or
// #atproto_space_host service to support spaces: the proposal lets a PDS serve
// the protocol at its ordinary #atproto_pds endpoint with no separate
// advertisement. So capability is detected empirically instead of by reading
// the DID document: probe getSpace with no params, a call every implementation
// recognizes, and only treat it as supported when the response is one only a
// real implementation would give — a successful result, or the XRPC
// "InvalidRequest" a lexicon-validating server returns for the missing
// required "space" param (e.g. `{"error":"InvalidRequest","message":"Invalid
// com.atproto.simplespace.getSpace params: Missing required key \"space\""}`
// from a real spaces-alpha PDS). Any other response — a 404/501, a generic
// router error, or some other error name entirely — is treated as
// unsupported, since it isn't a signal a spaces implementation must produce.
func SupportsSpaces(ctx context.Context, client *http.Client, pdsEndpoint string) bool {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		pdsEndpoint+"/xrpc/com.atproto.simplespace.getSpace",
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
