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

// unsupportedSpaceErrors are the XRPC error names (and, generically, HTTP
// statuses) a server returns for a method it does not implement at all, as
// opposed to a semantic error from a method it recognizes and applied its own
// validation or business logic to.
var unsupportedSpaceErrorNames = map[string]bool{
	"MethodNotImplemented": true,
	"XRPCNotSupported":     true,
	"NotFound":             true,
}

// SupportsSpaces reports whether the PDS at pdsEndpoint implements the atproto
// permissioned-data ("spaces") protocol — com.atproto.simplespace — per the
// alpha proposal (github.com/bluesky-social/proposals, 0016-permissioned-data).
// An identity is not required to advertise a dedicated #atproto_space or
// #atproto_space_host service to support spaces: the proposal lets a PDS serve
// the protocol at its ordinary #atproto_pds endpoint with no separate
// advertisement. So capability is detected empirically instead of by reading
// the DID document: probe a read-only method every implementation exposes
// (getSpace) and classify the response. A PDS that doesn't recognize the
// method responds with an HTTP 404/501 or the XRPC "method not implemented"
// error family; a PDS that does implement it responds with its own semantic
// error (invalid params, space not found, ...) or a successful result —
// either way, calling the method didn't fail because the method is unknown.
func SupportsSpaces(ctx context.Context, client *http.Client, pdsEndpoint string) bool {
	req, err := http.NewRequestWithContext(
		ctx, http.MethodGet, pdsEndpoint+"/xrpc/com.atproto.simplespace.getSpace", nil,
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

	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented {
		return false
	}

	var body struct {
		Error string `json:"error"`
	}
	// A response body that isn't a structured XRPC error (or isn't JSON at
	// all) still means the request reached a handler for the method, since an
	// unrecognized method fails closed with 404/501 above before a body
	// matters.
	_ = json.NewDecoder(resp.Body).Decode(&body)
	return !unsupportedSpaceErrorNames[body.Error]
}
