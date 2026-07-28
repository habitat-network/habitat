package oauthserver

import (
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"github.com/habitat-network/habitat/internal/pdsclient"
)

// loopbackClientIDOrigin is the fixed origin that identifies a loopback
// client ID, per https://atproto.com/specs/oauth#localhost-client-development.
const loopbackClientIDOrigin = "http://localhost"

// atprotoScopeValue is the scope token that every atproto loopback client ID
// must include.
const atprotoScopeValue = "atproto"

// defaultLoopbackRedirectURIs are used when a loopback client ID's query
// string omits redirect_uri.
var defaultLoopbackRedirectURIs = []string{"http://127.0.0.1/", "http://[::1]/"}

// isLoopbackClientID reports whether id is a loopback client ID: a
// self-describing, synthetic client identifier that must never be fetched
// over HTTP. Per RFC 8252 §8.3 it must use the "localhost" hostname
// (loopback IPs are reserved for redirect_uri instead).
func isLoopbackClientID(id string) bool {
	return strings.HasPrefix(id, loopbackClientIDOrigin)
}

// synthesizeLoopbackClientMetadata parses a loopback client ID and builds the
// client metadata document that would otherwise have been fetched from the
// client_id URL. See
// https://atproto.com/specs/oauth#client-id-metadata-document and RFC 8252
// §8.3 for the loopback redirect URI restrictions.
func synthesizeLoopbackClientMetadata(id string) (*pdsclient.ClientMetadata, error) {
	scope, redirectURIs, err := parseLoopbackClientID(id)
	if err != nil {
		return nil, err
	}
	return &pdsclient.ClientMetadata{
		ClientId:                id,
		Scope:                   scope,
		RedirectUris:            redirectURIs,
		ResponseTypes:           []string{"code"},
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		TokenEndpointAuthMethod: "none",
		ApplicationType:         "native",
		DpopBoundAccessTokens:   true,
	}, nil
}

// parseLoopbackClientID validates id as a loopback client ID and extracts its
// scope and redirect_uris, applying atproto defaults where the corresponding
// query parameter is omitted.
func parseLoopbackClientID(id string) (scope string, redirectURIs []string, err error) {
	if !strings.HasPrefix(id, loopbackClientIDOrigin) {
		return "", nil, fmt.Errorf("value must start with %q", loopbackClientIDOrigin)
	}
	rest := id[len(loopbackClientIDOrigin):]

	if strings.Contains(rest, "#") {
		return "", nil, errors.New("value must not contain a hash component")
	}

	// Since a path component is not allowed (except for a single "/"), the
	// query string starts either right after the origin or after a single
	// leading "/".
	rest = strings.TrimPrefix(rest, "/")
	var rawQuery string
	switch {
	case rest == "":
		rawQuery = ""
	case strings.HasPrefix(rest, "?"):
		rawQuery = rest[1:]
	default:
		return "", nil, errors.New("value must not contain a path component")
	}

	return parseLoopbackClientIDQuery(rawQuery)
}

// parseLoopbackClientIDQuery parses and validates the query string of a
// loopback client ID, applying atproto defaults for omitted parameters.
func parseLoopbackClientIDQuery(rawQuery string) (scope string, redirectURIs []string, err error) {
	values, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "", nil, fmt.Errorf("invalid query string: %w", err)
	}

	for key := range values {
		if key != "scope" && key != "redirect_uri" {
			return "", nil, fmt.Errorf("unexpected query parameter %q", key)
		}
	}

	scope = atprotoScopeValue
	if scopes, ok := values["scope"]; ok {
		if len(scopes) > 1 {
			return "", nil, errors.New(`duplicate "scope" query parameter`)
		}
		scope = scopes[0]
		if !isValidOAuthScope(scope) {
			return "", nil, fmt.Errorf("invalid %q query parameter: invalid OAuth scope", "scope")
		}
		if !containsScopeValue(scope, atprotoScopeValue) {
			return "", nil, fmt.Errorf(
				`atproto loopback client ID must include %q scope value`,
				atprotoScopeValue,
			)
		}
	}

	redirectURIs = defaultLoopbackRedirectURIs
	if uris, ok := values["redirect_uri"]; ok {
		redirectURIs = make([]string, len(uris))
		for i, uri := range uris {
			if err := validateLoopbackRedirectURI(uri); err != nil {
				return "", nil, fmt.Errorf("invalid %q query parameter: %w", "redirect_uri", err)
			}
			redirectURIs[i] = uri
		}
	}

	return scope, redirectURIs, nil
}

// isValidOAuthScope reports whether s is a syntactically valid OAuth scope
// value: a space-separated list of scope-tokens, each a non-empty run of
// printable ASCII characters excluding space, '"', and '\'.
func isValidOAuthScope(s string) bool {
	if s == "" {
		return false
	}
	for tok := range strings.SplitSeq(s, " ") {
		if !isValidScopeToken(tok) {
			return false
		}
	}
	return true
}

// isValidScopeToken reports whether tok is a valid scope-token: 1*( %x21 /
// %x23-5B / %x5D-7E ).
func isValidScopeToken(tok string) bool {
	if tok == "" {
		return false
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if c == 0x21 || (c >= 0x23 && c <= 0x5B) || (c >= 0x5D && c <= 0x7E) {
			continue
		}
		return false
	}
	return true
}

// containsScopeValue reports whether space-separated scope string s includes
// value as one of its scope-tokens.
func containsScopeValue(s, value string) bool {
	return slices.Contains(strings.Split(s, " "), value)
}

// validateLoopbackRedirectURI validates a redirect_uri value for a loopback
// client ID: it must be an http:// URI whose host is a loopback IP literal
// (127.0.0.1 or [::1]). Per RFC 8252 §8.3, "localhost" is explicitly
// disallowed here even though it is a valid loopback hostname elsewhere.
func validateLoopbackRedirectURI(raw string) error {
	if !strings.HasPrefix(raw, "http://") {
		return errors.New(`URL must use the "http:" protocol`)
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Hostname() {
	case "127.0.0.1", "::1":
		return nil
	case "localhost":
		return errors.New(
			`use of "localhost" hostname is not allowed (RFC 8252), use a loopback IP such as "127.0.0.1" instead`,
		)
	default:
		return errors.New(`URL must use "127.0.0.1" or "[::1]" as hostname`)
	}
}
