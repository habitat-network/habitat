package httpx

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"unicode"

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

// ParseServiceRefInput parses a service identifier — a DID with an optional
// service fragment, e.g. "did:web:syncer.example.com#habitat_space_syncer" —
// as used to name a service in a DID document (see the xrpc
// inter-service-authentication spec). The fragment is optional: a bare DID
// names an account, and the returned service ID is then the conventional
// personal-data-server entry.
//
// The DID half is validated by syntax.ParseDID. The fragment is checked only
// for the things that would make the reference ambiguous or unresolvable —
// empty, containing a further "#", or containing whitespace — since whether a
// given fragment names a real service is settled by resolving the document,
// not by validating the string.
func ParseServiceRefInput(
	ctx context.Context,
	w http.ResponseWriter,
	input string,
	name string,
) (syntax.DID, string, bool) {
	rawDID, serviceID, hasFragment := strings.Cut(input, "#")
	did, ok := ParseDIDInput(ctx, w, rawDID, name)
	if !ok {
		return "", "", false
	}
	if !hasFragment {
		return did, atprotoPDSService, true
	}
	// A second "#" would make the fragment ambiguous, and whitespace has no
	// place in either the DID or the fragment.
	if serviceID == "" || strings.ContainsRune(serviceID, '#') || strings.ContainsFunc(
		input,
		unicode.IsSpace,
	) {
		WriteInvalidRequest(
			ctx, w, "malformed "+name, fmt.Errorf("invalid service fragment in %q", input),
		)
		return "", "", false
	}
	return did, serviceID, true
}

// atprotoPDSService is the DID document service a bare service DID resolves
// to, since a bare DID names an account and an account is served by its PDS.
const atprotoPDSService = "atproto_pds"
