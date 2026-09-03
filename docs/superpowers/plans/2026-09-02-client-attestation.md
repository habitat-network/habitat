# Client Attestation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Habitat's `getSpaceCredential` endpoint verify AT Proto client-attestation JWTs and enforce a per-space app allow-list, closing the existing `NotSupported` stub.

**Architecture:** A new `network.habitat.space.appAccess` record collection (one record per allowed OAuth `client_id`, written via two new dedicated procedures) drives enforcement in `GetSpaceCredential`. A new shared `internal/clientmeta` package resolves a client's published `client-metadata.json` + JWKS (extracted from `internal/oauthserver`, which is refactored to use it), and a new attestation verifier in `internal/spaces/server` uses that resolver to verify the JWT's signature and claims per the proposal.

**Tech Stack:** Go 1.26, `github.com/go-jose/go-jose/v3` + `.../jwt` for attestation JWT parsing/verification, `github.com/bluesky-social/indigo/atproto/atcrypto` and `.../auth/oauth` for key/metadata types, GORM-backed `internal/spaces` store, `moon :generate` (lexgen) for lexicon codegen.

**Spec:** `docs/superpowers/specs/2026-09-02-client-attestation-design.md`

## Global Constraints

- Verification only — Habitat never signs/produces a client attestation itself (spec: Out of scope).
- No replay protection / `jti` store — `jti` is required and format-checked only (spec: Out of scope).
- The simplespace `policy` (user-authorization) concept is untouched — only `appAccess` (app identity) is in scope (spec: Out of scope).
- Attestation JWT header: `{"typ": "atproto-client-attestation+jwt", "alg": "ES256", "kid": "..."}`; payload: `{iss, sub, aud, iat, exp, jti}` with `iss == sub == client_id` and `aud == "<spaceOwnerDID>#atproto_space_host"` (spec: Attestation JWT verification).
- `appAccess` grant records: collection `network.habitat.space.appAccess`, one record per allowed `client_id`, `rkey` = base64url-no-padding of the `client_id`, written to the space owner's repo. Presence of any record in the collection puts the space in allow-list mode; an empty collection means open (spec: Data model).
- Grant writes are gated to `SpaceRoleManager`/`SpaceRoleOwner` via dedicated procedures (not generic `PutRecord`, which only requires `SpaceRoleWriter` and restricts writes to the caller's own repo) (spec: Data model, and plan-time correction below).
- No generated file (`api/habitat/*.go`, `typescript/api/*`) is hand-edited — always regenerate via `moon :generate` (repo convention, `CLAUDE.md`).

---

## Plan-time correction to the spec

The spec's "Data model" section said grants would be written via the **existing generic** `PutRecord`/`DeleteRecord` procedures. Reading those handlers (`internal/spaces/server/server.go`) during planning showed that's not viable: `PutRecord` requires `repo == credInfo.Subject` (a member writing only their own repo) and authorizes on `SpaceRoleWriter`, not `SpaceRoleManager`/`SpaceRoleOwner`. An `appAccess` grant is a space-level config, not any one member's data, and must be Manager/Owner-gated per the spec's stated intent.

This plan instead follows the existing precedent for exactly this kind of space-level (not per-repo) record: `network.habitat.relationship.setSpaceRelation`/`deleteRelation` (`internal/relationship/server.go`), which authorizes on `SpaceRoleManager` and writes into the *space owner's* repo via `spaces.Store.PutRecord` directly, bypassing the generic HTTP procedure. Task 1 adds `network.habitat.space.appAccess` to `internal/syntax.ReservedCollections` (blocking it from generic `PutRecord`/`DeleteRecord`, matching how relationship-tuple collections are reserved today), and Task 4 adds two new dedicated procedures, `addAppAccess`/`removeAppAccess`, mirroring that pattern.

---

### Task 1: Lexicons — `appAccess` record + `addAppAccess`/`removeAppAccess` procedures

**Files:**
- Create: `lexicons/network/habitat/space/appAccess.json`
- Create: `lexicons/network/habitat/space/addAppAccess.json`
- Create: `lexicons/network/habitat/space/removeAppAccess.json`
- Modify: `internal/syntax/reserved.go`
- Create: `internal/syntax/reserved_test.go` (if it doesn't already exist — check first)

**Interfaces:**
- Produces: `syntax.NSID` constant `habitat_syntax.AppAccessCollection = "network.habitat.space.appAccess"`, added to `habitat_syntax.ReservedCollections`. Generated Go types `habitat.NetworkHabitatSpaceAddAppAccessInput{Space, ClientId string}` / `Output{Uri string}`, `habitat.NetworkHabitatSpaceRemoveAppAccessInput{Space, ClientId string}` / `Output{}` (used by Task 4).

- [ ] **Step 1: Write the record lexicon**

`lexicons/network/habitat/space/appAccess.json`:

```json
{
  "lexicon": 1,
  "id": "network.habitat.space.appAccess",
  "defs": {
    "main": {
      "type": "record",
      "description": "Grants an OAuth client (identified by its client_id) access to a space that gates on app identity. Owned by the org repo within the space it governs. The record key is the base64url (no padding) encoding of the client_id; the client identity is recoverable from the key alone, so no fields are required. A space with at least one appAccess record is in allow-list mode: getSpaceCredential requires and verifies a client attestation, and the attested client_id must have a matching record. A space with none is open.",
      "key": "any",
      "record": {
        "type": "object",
        "properties": {
          "note": {
            "type": "string",
            "description": "Optional human-readable label for this grant, for admin display. Not used for enforcement.",
            "maxLength": 640
          },
          "createdAt": {
            "type": "string",
            "format": "datetime"
          }
        }
      }
    }
  }
}
```

- [ ] **Step 2: Write the `addAppAccess` procedure lexicon**

`lexicons/network/habitat/space/addAppAccess.json`:

```json
{
  "lexicon": 1,
  "id": "network.habitat.space.addAppAccess",
  "defs": {
    "main": {
      "type": "procedure",
      "description": "Grant an OAuth client access to a space, creating the space's appAccess allow-list if this is its first grant. Caller must have the manager role on the space. The grant record is owned by the org repo within its governing space.",
      "input": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "required": ["space", "clientId"],
          "properties": {
            "space": {
              "type": "string",
              "format": "at-uri",
              "description": "Reference to the space."
            },
            "clientId": {
              "type": "string",
              "format": "uri",
              "description": "The OAuth client_id (client metadata document URL) to grant access to.",
              "maxLength": 384
            }
          }
        }
      },
      "output": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "required": ["uri"],
          "properties": {
            "uri": {
              "type": "string",
              "format": "at-uri",
              "description": "URI of the written appAccess record."
            }
          }
        }
      },
      "errors": [
        { "name": "SpaceNotFound" },
        {
          "name": "InvalidClientId",
          "description": "clientId is not a valid OAuth client_id URL, or is too long to address as a record key."
        }
      ]
    }
  }
}
```

- [ ] **Step 3: Write the `removeAppAccess` procedure lexicon**

`lexicons/network/habitat/space/removeAppAccess.json`:

```json
{
  "lexicon": 1,
  "id": "network.habitat.space.removeAppAccess",
  "defs": {
    "main": {
      "type": "procedure",
      "description": "Revoke an OAuth client's access to a space, or ensure it isn't granted. Caller must have the manager role on the space. If this removes the space's last appAccess grant, the space reverts to open access.",
      "input": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "required": ["space", "clientId"],
          "properties": {
            "space": {
              "type": "string",
              "format": "at-uri",
              "description": "Reference to the space."
            },
            "clientId": {
              "type": "string",
              "format": "uri",
              "description": "The OAuth client_id (client metadata document URL) to revoke access from.",
              "maxLength": 384
            }
          }
        }
      },
      "output": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "properties": {}
        }
      },
      "errors": [
        { "name": "SpaceNotFound" },
        {
          "name": "InvalidClientId",
          "description": "clientId is too long to address as a record key."
        }
      ]
    }
  }
}
```

- [ ] **Step 4: Regenerate lexicon-derived code**

Run: `moon :generate`

This regenerates `api/habitat/space_appAccess.go`, `api/habitat/space_addAppAccess.go`, `api/habitat/space_removeAppAccess.go` (Go), the matching `typescript/api/types/network/habitat/space/*.ts`, and the OpenAPI spec. Do not hand-edit any of these.

- [ ] **Step 5: Add the reserved-collection constant**

Read `internal/syntax/reserved.go` and `internal/syntax/relationship.go` first (to match the existing constant style), then edit `internal/syntax/reserved.go`:

```go
package syntax

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

// AppAccessCollection is the collection network.habitat.space.appAccess
// grant records are written into. See ReservedCollections.
const AppAccessCollection syntax.NSID = "network.habitat.space.appAccess"

var (
	ReservedCollections = xmaps.Set[syntax.NSID]{
		UserRelationCollection:  struct{}{},
		SpaceRelationCollection: struct{}{},
		AppAccessCollection:     struct{}{},
	}
)
```

- [ ] **Step 6: Write a test pinning the reservation**

Check whether `internal/syntax/reserved_test.go` exists; if not, create it:

```go
package syntax_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestReservedCollectionsIncludesAppAccess(t *testing.T) {
	require.True(t, habitat_syntax.ReservedCollections.Contains(habitat_syntax.AppAccessCollection))
	require.Equal(
		t,
		"network.habitat.space.appAccess",
		string(habitat_syntax.AppAccessCollection),
	)
}
```

If the file already exists with a similar test for the other two collections, add this case there instead of creating a new file, following the existing style.

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/syntax/... -run TestReservedCollectionsIncludesAppAccess -v`
Expected: PASS

- [ ] **Step 8: Build to confirm generated code compiles**

Run: `go build ./...`
Expected: success (confirms the generated `habitat.NetworkHabitatSpace{AddAppAccess,RemoveAppAccess,AppAccess}*` types exist and are well-formed; nothing references them yet).

- [ ] **Step 9: Commit**

```bash
git add lexicons/network/habitat/space/appAccess.json \
  lexicons/network/habitat/space/addAppAccess.json \
  lexicons/network/habitat/space/removeAppAccess.json \
  api/habitat typescript/api api-docs \
  internal/syntax/reserved.go internal/syntax/reserved_test.go
git commit -m "$(cat <<'EOF'
Add appAccess lexicons for client attestation allow-lists

Defines network.habitat.space.appAccess (a record granting an OAuth
client access to a space) plus addAppAccess/removeAppAccess
procedures to manage it, and reserves the collection from generic
PutRecord/DeleteRecord.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JGjST45wbdpQ471zfarFBS
EOF
)"
```

---

### Task 2: Shared `internal/clientmeta` resolver package

**Files:**
- Create: `internal/clientmeta/resolver.go`
- Create: `internal/clientmeta/resolver_test.go`
- Create: `internal/clientmeta/localhost.go` (moved from `internal/oauthserver/localhost.go`)
- Create: `internal/clientmeta/localhost_test.go` (moved from `internal/oauthserver/storage_test.go`'s `TestGetClientLocalhost` localhost-synthesis subtests — see Step 6)
- Create: `internal/clientmeta/jwk.go` (moved from `internal/oauthserver/fosite_storage.go`'s `atcryptoJWKtoJose`)
- Create: `internal/clientmeta/jwk_test.go` (moved from `internal/oauthserver/atcrypto_jwk_test.go`)
- Delete: `internal/oauthserver/localhost.go`
- Delete: `internal/oauthserver/atcrypto_jwk_test.go`
- Modify: `internal/oauthserver/fosite_storage.go` (use `internal/clientmeta` instead of local copies)
- Modify: `internal/oauthserver/oauth_server.go` (construct and pass the resolver, if `newStore` is called from there — check first)
- Modify: `internal/oauthserver/storage_test.go` (remove the moved localhost subtests, keep `TestGetClient`/the non-localhost parts of `TestGetClientLocalhost` that exercise `store.GetClient`)

**Interfaces:**
- Produces:
  - `clientmeta.Resolver` — `func NewResolver() *Resolver`
  - `func (r *Resolver) FetchMetadata(ctx context.Context, clientID string) (*oauth.ClientMetadata, error)`
  - `func (r *Resolver) ResolveKey(ctx context.Context, clientID, kid string) (*jose.JSONWebKey, error)` — fetches metadata, then looks in inline `jwks` or fetches `jwks_uri`, and converts the matching key
  - `clientmeta.ErrKeyNotFound` — sentinel returned by `ResolveKey` when no key matches
  - `clientmeta.ConvertJWK(jwk atcrypto.JWK) (*jose.JSONWebKey, error)` (exported rename of `atcryptoJWKtoJose`)
- Consumes: nothing from earlier tasks.

- [ ] **Step 1: Read the files being moved**

Read `internal/oauthserver/localhost.go`, `internal/oauthserver/fosite_storage.go` (specifically `fetchClientMetadata`, `GetPublicKeys`, `GetPublicKey`, `atcryptoJWKtoJose`), and `internal/oauthserver/atcrypto_jwk_test.go` in full before editing — they're being relocated, and the moved code must stay byte-for-byte equivalent except for the package name, receiver removal, and the new `jwks_uri` fetch path added in Step 3.

- [ ] **Step 2: Move localhost synthesis verbatim**

Create `internal/clientmeta/localhost.go` with the exact contents of `internal/oauthserver/localhost.go`, changing only the `package` line:

```go
package clientmeta

import (
	"fmt"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
)

// Localhost development clients don't publish a metadata document; the
// document is synthesized from the client_id URL instead. See
// https://atproto.com/specs/oauth#localhost-client-development.
var (
	// defaultLocalhostRedirectUris are used when the client_id carries no
	// redirect_uri query parameter.
	defaultLocalhostRedirectUris = []string{"http://127.0.0.1/", "http://[::1]/"}
	defaultLocalhostScope        = "atproto"
	localhostClientName          = "Development client"
)

// isLocalhostClientId reports whether a client_id URL is a localhost
// development client_id, i.e. one whose metadata is synthesized rather than
// fetched. The hostname must be exactly `localhost`; loopback IP literals are
// not localhost client_ids.
func isLocalhostClientId(u *url.URL) bool {
	return u.Scheme == "http" && u.Hostname() == "localhost"
}

// localhostClientMetadata builds the virtual client metadata document for a
// localhost development client_id. The client_id origin must be exactly
// `http://localhost` with no port and an empty path; redirect_uri (repeatable)
// and scope (single) may be supplied as query parameters. Such a client is
// always public: it authenticates with no secret.
//
// See https://atproto.com/specs/oauth#localhost-client-development.
func localhostClientMetadata(id string, u *url.URL) (*oauth.ClientMetadata, error) {
	if u.Port() != "" {
		return nil, fmt.Errorf("localhost client id must not specify a port")
	}
	if u.Path != "" && u.Path != "/" {
		return nil, fmt.Errorf("localhost client id must have an empty path")
	}
	if u.User != nil {
		return nil, fmt.Errorf("localhost client id must not contain userinfo")
	}
	if u.Fragment != "" {
		return nil, fmt.Errorf("localhost client id must not contain a fragment")
	}

	query, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse localhost client id query: %w", err)
	}
	for param := range query {
		if param != "redirect_uri" && param != "scope" {
			return nil, fmt.Errorf("unsupported localhost client id parameter %q", param)
		}
	}

	scope := defaultLocalhostScope
	if scopes := query["scope"]; len(scopes) > 1 {
		return nil, fmt.Errorf("localhost client id must have at most one scope parameter")
	} else if len(scopes) == 1 {
		scope = scopes[0]
	}

	redirectUris := query["redirect_uri"]
	if len(redirectUris) == 0 {
		redirectUris = defaultLocalhostRedirectUris
	}
	for _, redirectUri := range redirectUris {
		if err := validateLoopbackRedirectUri(redirectUri); err != nil {
			return nil, err
		}
	}

	return &oauth.ClientMetadata{
		ClientID:                id,
		ClientName:              new(localhostClientName),
		ApplicationType:         new("native"),
		GrantTypes:              []string{"authorization_code", "refresh_token"},
		ResponseTypes:           []string{"code"},
		RedirectURIs:            redirectUris,
		Scope:                   scope,
		TokenEndpointAuthMethod: "none",
		DPoPBoundAccessTokens:   true,
	}, nil
}

// validateLoopbackRedirectUri checks that a localhost client's declared
// redirect URI points at the loopback interface by IP literal. The port is
// deliberately unconstrained: the client is matched against these URIs ignoring
// the port (see fosite's RFC 8252 §7.3 loopback matching), so a dev server on
// any port works.
func validateLoopbackRedirectUri(redirectUri string) error {
	u, err := url.Parse(redirectUri)
	if err != nil {
		return fmt.Errorf("failed to parse localhost client redirect uri: %w", err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("localhost client redirect uri must use the http scheme")
	}
	if u.Hostname() != "127.0.0.1" && u.Hostname() != "::1" {
		return fmt.Errorf("localhost client redirect uri must use a loopback IP address")
	}
	return nil
}
```

Note: `new(localhostClientName)` and `new("native")` rely on a project-local generic `new` helper already used by `oauth_server.go`/`localhost.go` in this codebase (returns `*T` for a value) — check `internal/oauthserver` for where that helper is defined (likely a small package-local `func new[T any](v T) *T`) and copy it into `internal/clientmeta` too if it isn't already available from an imported shared package. Grep first: `grep -rn "^func new\[" internal/oauthserver/`.

Delete `internal/oauthserver/localhost.go`.

- [ ] **Step 3: Move JWK conversion, exported**

Create `internal/clientmeta/jwk.go`:

```go
package clientmeta

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"fmt"
	"math/big"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	jose "github.com/go-jose/go-jose/v3"
)

// ConvertJWK converts an atproto JWK (an EC public key, as used in client
// metadata documents) into a go-jose JSONWebKey usable for signature
// verification. Only the curves go-jose understands are supported; ES256
// (and this package's callers, which all verify ES256-signed JWTs) never use
// secp256k1, so it is rejected.
func ConvertJWK(jwk atcrypto.JWK) (*jose.JSONWebKey, error) {
	var curve elliptic.Curve
	switch jwk.Curve {
	case "P-256":
		curve = elliptic.P256()
	case "P-384":
		curve = elliptic.P384()
	case "P-521":
		curve = elliptic.P521()
	default:
		return nil, fmt.Errorf("unsupported JWK curve %q", jwk.Curve)
	}
	if jwk.KeyType != "EC" {
		return nil, fmt.Errorf("unsupported JWK key type %q", jwk.KeyType)
	}
	x, err := base64.RawURLEncoding.DecodeString(jwk.X)
	if err != nil {
		return nil, fmt.Errorf("invalid JWK x coordinate: %w", err)
	}
	y, err := base64.RawURLEncoding.DecodeString(jwk.Y)
	if err != nil {
		return nil, fmt.Errorf("invalid JWK y coordinate: %w", err)
	}
	var keyID string
	if jwk.KeyID != nil {
		keyID = *jwk.KeyID
	}
	return &jose.JSONWebKey{
		//nolint:staticcheck // SA1019: deprecated ecdsa.PublicKey X/Y fields
		Key: &ecdsa.PublicKey{
			Curve: curve,
			X:     new(big.Int).SetBytes(x),
			Y:     new(big.Int).SetBytes(y),
		},
		KeyID: keyID,
	}, nil
}
```

Create `internal/clientmeta/jwk_test.go` with the exact contents of `internal/oauthserver/atcrypto_jwk_test.go`, changing the package to `clientmeta` and every call site from `atcryptoJWKtoJose(...)` to `ConvertJWK(...)`.

Delete `internal/oauthserver/atcrypto_jwk_test.go`.

- [ ] **Step 4: Run the moved tests**

Run: `go test ./internal/clientmeta/... -v`
Expected: `TestAtcryptoJWKtoJose` (all subtests) PASS. (Rename the test function to `TestConvertJWK` while moving it, for consistency with the new exported name.)

- [ ] **Step 5: Write the resolver, including new `jwks_uri` support**

Create `internal/clientmeta/resolver.go`:

```go
// Package clientmeta resolves AT Proto OAuth client-id-metadata-documents
// and their published JWKS, for verifying JWTs a client signs with its own
// key: RFC 7523 JWT-bearer client authentication (internal/oauthserver) and
// AT Proto client attestation (internal/spaces/server). See
// https://atproto.com/specs/oauth#client-id-metadata-document.
package clientmeta

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/httpx"
)

// ErrKeyNotFound is returned by ResolveKey when the client's published JWKS
// (inline or via jwks_uri) has no key matching the requested kid.
var ErrKeyNotFound = errors.New("no matching key in client jwks")

// Resolver fetches client metadata documents and JWKS over HTTP. The zero
// value is ready to use.
type Resolver struct{}

// NewResolver constructs a Resolver.
func NewResolver() *Resolver {
	return &Resolver{}
}

// FetchMetadata fetches and decodes the client metadata document published
// at clientID (the client's client_id URL). Localhost development client_ids
// are the exception: nothing is fetched, the metadata is derived from the
// client_id itself. See
// https://atproto.com/specs/oauth#localhost-client-development.
func (r *Resolver) FetchMetadata(ctx context.Context, clientID string) (*oauth.ClientMetadata, error) {
	parsed, err := url.Parse(clientID)
	if err != nil {
		return nil, fmt.Errorf("failed to parse client id: %w", err)
	}
	if isLocalhostClientId(parsed) {
		return localhostClientMetadata(clientID, parsed)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, clientID, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}
	// TODO: consider caching
	cl := httpx.NewClient()
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch client metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch client metadata: status %d", resp.StatusCode)
	}

	var metadata oauth.ClientMetadata
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return nil, fmt.Errorf("failed to decode client metadata: %w", err)
	}
	return &metadata, nil
}

// fetchJWKS returns metadata's JWKS keys, from the inline jwks field if
// present, otherwise by fetching jwks_uri. Returns an empty, nil-error result
// if the client publishes neither.
func (r *Resolver) fetchJWKS(ctx context.Context, metadata *oauth.ClientMetadata) (*oauth.JWKS, error) {
	if metadata.JWKS != nil {
		return metadata.JWKS, nil
	}
	if metadata.JWKSURI == nil || *metadata.JWKSURI == "" {
		return nil, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, *metadata.JWKSURI, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to make jwks_uri request: %w", err)
	}
	cl := httpx.NewClient()
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch jwks_uri: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch jwks_uri: status %d", resp.StatusCode)
	}

	var jwks oauth.JWKS
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("failed to decode jwks_uri response: %w", err)
	}
	return &jwks, nil
}

// ResolveKey fetches clientID's metadata and returns the key identified by
// kid from its published JWKS (inline jwks, or fetched from jwks_uri),
// converted to a go-jose key usable for signature verification. Returns
// ErrKeyNotFound if the client has no matching key.
func (r *Resolver) ResolveKey(ctx context.Context, clientID, kid string) (*jose.JSONWebKey, error) {
	metadata, err := r.FetchMetadata(ctx, clientID)
	if err != nil {
		return nil, err
	}
	jwks, err := r.fetchJWKS(ctx, metadata)
	if err != nil {
		return nil, err
	}
	if jwks == nil {
		return nil, ErrKeyNotFound
	}
	for _, key := range jwks.Keys {
		if key.KeyID == nil || *key.KeyID != kid {
			continue
		}
		return ConvertJWK(key)
	}
	return nil, ErrKeyNotFound
}
```

Add the missing `jose "github.com/go-jose/go-jose/v3"` import to the import block above.

- [ ] **Step 6: Write resolver tests**

Move the localhost-behavior subtests out of `internal/oauthserver/storage_test.go`'s `TestGetClientLocalhost` (the `t.Run("defaults", ...)`, `t.Run("query parameters", ...)`, `t.Run("redirect uri port is not matched", ...)`, and `t.Run("rejected client ids", ...)` blocks) into a new `internal/clientmeta/localhost_test.go`, rewritten to call `(&Resolver{}).FetchMetadata` directly instead of `store.GetClient`, and asserting on the returned `*oauth.ClientMetadata` fields directly instead of via the fosite `client` wrapper:

```go
package clientmeta

import (
	"context"
	"net/url"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolverFetchMetadataLocalhost(t *testing.T) {
	r := NewResolver()

	t.Run("defaults", func(t *testing.T) {
		metadata, err := r.FetchMetadata(context.Background(), "http://localhost")
		require.NoError(t, err)

		require.Equal(t, "http://localhost", metadata.ClientID)
		require.Equal(t, "none", metadata.TokenEndpointAuthMethod)
		require.Equal(t, []string{"http://127.0.0.1/", "http://[::1]/"}, metadata.RedirectURIs)
		require.Equal(t, "atproto", metadata.Scope)
		require.Equal(t, []string{"code"}, metadata.ResponseTypes)
		require.Equal(t, []string{"authorization_code", "refresh_token"}, metadata.GrantTypes)
	})

	t.Run("query parameters", func(t *testing.T) {
		clientId := "http://localhost/?" + url.Values{
			"redirect_uri": {"http://127.0.0.1/callback", "http://[::1]/callback"},
			"scope":        {"atproto transition:generic"},
		}.Encode()

		metadata, err := r.FetchMetadata(context.Background(), clientId)
		require.NoError(t, err)

		require.Equal(t, clientId, metadata.ClientID)
		require.Equal(
			t,
			[]string{"http://127.0.0.1/callback", "http://[::1]/callback"},
			metadata.RedirectURIs,
		)
		require.Equal(t, "atproto transition:generic", metadata.Scope)
	})

	// Ids that are not localhost client ids at all (https scheme, or a
	// loopback IP rather than the localhost hostname) are not listed here:
	// those fall through to a regular client metadata document fetch.
	t.Run("rejected client ids", func(t *testing.T) {
		for _, clientId := range []string{
			"http://localhost:8080",                 // explicit port
			"http://localhost/client-metadata.json", // non-empty path
			"http://user@localhost",                 // userinfo
			"http://localhost/?scope=a&scope=b",     // multiple scope params
			"http://localhost/?foo=bar",             // unsupported parameter
			"http://localhost/?redirect_uri=" + url.QueryEscape("https://example.com/cb"),
			"http://localhost/?redirect_uri=" + url.QueryEscape("http://localhost/cb"),
		} {
			t.Run(clientId, func(t *testing.T) {
				_, err := r.FetchMetadata(context.Background(), clientId)
				require.Error(t, err)
			})
		}
	})
}
```

Leave the "redirect uri port is not matched" subtest in `internal/oauthserver`, since it exercises `fosite.MatchRedirectURIWithClientRedirectURIs`, a fosite-specific concern unrelated to `clientmeta`.

Add `internal/clientmeta/resolver_test.go` covering the new `jwks_uri` path (the genuinely new behavior this task adds beyond the move):

```go
package clientmeta

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/stretchr/testify/require"
)

func testJWK(t *testing.T, kid string) (atcrypto.JWK, atcrypto.PublicKey) {
	t.Helper()
	priv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	pub, err := priv.PublicKey()
	require.NoError(t, err)
	jwk, err := pub.JWK()
	require.NoError(t, err)
	jwk.KeyID = &kid
	return *jwk, pub
}

func TestResolverResolveKeyInlineJWKS(t *testing.T) {
	jwk, _ := testJWK(t, "key-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: "http://" + r.Host + "/client-metadata.json",
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
		}))
	}))
	defer server.Close()

	key, err := NewResolver().ResolveKey(
		context.Background(), server.URL+"/client-metadata.json", "key-1",
	)
	require.NoError(t, err)
	require.Equal(t, "key-1", key.KeyID)
}

func TestResolverResolveKeyJWKSURI(t *testing.T) {
	jwk, _ := testJWK(t, "key-1")

	var jwksURL string
	mux := http.NewServeMux()
	mux.HandleFunc("/client-metadata.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: "http://" + r.Host + "/client-metadata.json",
			JWKSURI:  &jwksURL,
		}))
	})
	mux.HandleFunc("/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.JWKS{Keys: []atcrypto.JWK{jwk}}))
	})
	server := httptest.NewServer(mux)
	defer server.Close()
	jwksURL = server.URL + "/jwks.json"

	key, err := NewResolver().ResolveKey(
		context.Background(), server.URL+"/client-metadata.json", "key-1",
	)
	require.NoError(t, err)
	require.Equal(t, "key-1", key.KeyID)
}

func TestResolverResolveKeyNotFound(t *testing.T) {
	jwk, _ := testJWK(t, "key-1")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: "http://" + r.Host + "/client-metadata.json",
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
		}))
	}))
	defer server.Close()

	_, err := NewResolver().ResolveKey(
		context.Background(), server.URL+"/client-metadata.json", "wrong-kid",
	)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

func TestResolverResolveKeyNoJWKS(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: "http://" + r.Host + "/client-metadata.json",
		}))
	}))
	defer server.Close()

	_, err := NewResolver().ResolveKey(
		context.Background(), server.URL+"/client-metadata.json", "key-1",
	)
	require.ErrorIs(t, err, ErrKeyNotFound)
}

var _ = fmt.Sprintf // keep fmt import if unused after edits; remove if not needed
```

(Drop the trailing `var _ = fmt.Sprintf` line and the `fmt` import if the final file doesn't otherwise need `fmt`.)

- [ ] **Step 7: Run the new tests, verify they fail first, then pass**

Run: `go test ./internal/clientmeta/... -v`
Expected before `resolver.go` exists / is wired: build failure (package incomplete). Once Step 5's `resolver.go` is in place: all tests PASS.

- [ ] **Step 8: Refactor `internal/oauthserver` to use `clientmeta`**

Read `internal/oauthserver/fosite_storage.go` in full again at this point (it may have shifted). Edit it:

- Add field `clientMeta *clientmeta.Resolver` to `store` struct, and a parameter to `newStore` (check all call sites — likely just `internal/oauthserver/oauth_server.go` — and update them to pass `clientmeta.NewResolver()`).
- Replace the body of `fetchClientMetadata` with:

```go
func (s *store) fetchClientMetadata(ctx context.Context, id string) (*oauth.ClientMetadata, error) {
	return s.clientMeta.FetchMetadata(ctx, id)
}
```

- Replace `GetPublicKeys` to use `s.clientMeta.ResolveKey` for the loop over `metadata.JWKS.Keys`... actually simpler: since `GetPublicKeys` needs the *set* of keys (for `rfc7523.RFC7523KeyStorage.GetPublicKeys`), not a single `kid` lookup, keep its own key-set-building loop but replace the conversion call:

```go
func (s *store) GetPublicKeys(
	ctx context.Context,
	issuer string,
	_ string,
) (*jose.JSONWebKeySet, error) {
	if !s.approvedJwtBearerClients.IsApprovedClient(issuer) {
		return nil, fosite.ErrNotFound
	}
	metadata, err := s.fetchClientMetadata(ctx, issuer)
	if err != nil {
		return nil, err
	}
	if metadata.JWKS == nil || len(metadata.JWKS.Keys) == 0 {
		return nil, fosite.ErrNotFound
	}
	var keys []jose.JSONWebKey
	for _, key := range metadata.JWKS.Keys {
		if key.KeyID == nil {
			continue
		}
		converted, err := clientmeta.ConvertJWK(key)
		if err != nil {
			continue
		}
		keys = append(keys, *converted)
	}
	if len(keys) == 0 {
		return nil, fosite.ErrNotFound
	}
	return &jose.JSONWebKeySet{Keys: keys}, nil
}
```

- Delete the now-unused `atcryptoJWKtoJose` function body from `fosite_storage.go` (it moved to `clientmeta.ConvertJWK`) and its now-unused imports (`crypto/ecdsa`, `crypto/elliptic`, `math/big` — check each is truly unused after the edit before removing).
- Add `"github.com/habitat-network/habitat/internal/clientmeta"` to the import block.

- [ ] **Step 9: Update `internal/oauthserver/storage_test.go`**

Remove the subtests moved to `internal/clientmeta/localhost_test.go` from `TestGetClientLocalhost`, keeping only the "redirect uri port is not matched" subtest (renamed to its own `TestGetClientRedirectUriPortNotMatched` if `TestGetClientLocalhost` would otherwise be left with just one subtest — use judgment on whether keeping the wrapper name reads better). Update `newStore(testutil.NewDB(t), nil)` call sites to match the new `newStore` signature from Step 8 (pass `clientmeta.NewResolver()`).

- [ ] **Step 10: Run full test suites for both packages**

Run: `go test ./internal/clientmeta/... ./internal/oauthserver/... -v`
Expected: all PASS, including `TestGetClient` and the remaining subtest of `TestGetClientLocalhost`/its renamed replacement.

- [ ] **Step 11: Build the whole module**

Run: `go build ./...`
Expected: success.

- [ ] **Step 12: Commit**

```bash
git add internal/clientmeta internal/oauthserver
git commit -m "$(cat <<'EOF'
Extract client-metadata/JWKS resolution into internal/clientmeta

Moves client-id-metadata-document fetching, localhost-dev-client
synthesis, and atproto-JWK conversion out of internal/oauthserver
into a shared package, and adds jwks_uri support alongside the
existing inline jwks handling. internal/oauthserver is refactored to
use it with no behavior change; internal/spaces/server will use it
next for client attestation verification.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JGjST45wbdpQ471zfarFBS
EOF
)"
```

---

### Task 3: Attestation JWT verification

**Files:**
- Create: `internal/spaces/server/attestation.go`
- Create: `internal/spaces/server/attestation_test.go`

**Interfaces:**
- Consumes: `clientmeta.Resolver.ResolveKey(ctx, clientID, kid string) (*jose.JSONWebKey, error)` and `clientmeta.ErrKeyNotFound` (Task 2).
- Produces: `func verifyAttestation(ctx context.Context, resolver *clientmeta.Resolver, raw string, spaceOwner syntax.DID) (clientID string, err error)` and sentinel `var ErrInvalidAttestation = errors.New(...)` — both consumed by Task 5's `GetSpaceCredential` change.

- [ ] **Step 1: Write the failing tests**

Create `internal/spaces/server/attestation_test.go`:

```go
package spaces_server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/clientmeta"
)

const testSpaceOwner = syntax.DID("did:plc:owner")

// attestationTestServer serves a client-metadata.json (embedding the given
// public key under kid) at a URL usable as a client_id, and returns that
// client_id plus the matching private key for signing test attestations.
func attestationTestServer(t *testing.T, kid string) (clientID string, priv *ecdsa.PrivateKey) {
	t.Helper()
	akPriv, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	akPub, err := akPriv.PublicKey()
	require.NoError(t, err)
	jwk, err := akPub.JWK()
	require.NoError(t, err)
	jwk.KeyID = &kid

	var url string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: url,
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{*jwk}},
		}))
	}))
	t.Cleanup(server.Close)
	url = server.URL + "/client-metadata.json"

	// akPriv is an atcrypto key (HashAndSign, not a raw crypto.Signer), so
	// go-jose can't sign with it directly; extract the raw ecdsa key for
	// signing instead — the signature still verifies against the JWK above
	// since it's the same key pair.
	rawKey, ok := akPriv.(interface{ Bytes() []byte })
	require.True(t, ok)
	_ = rawKey // documents why the helper below exists; see genTestKeyPair
	return url, priv
}
```

Stop and reconsider: extracting the raw `*ecdsa.PrivateKey` back out of an `atcrypto.PrivateKeyP256` is not straightforward (its fields are unexported, and `PrivateKeyExportable.Bytes()` gives compact-encoded bytes, not something trivially fed to `ecdsa.PrivateKey`). Use a plain `crypto/ecdsa` key for signing instead, and build the `atcrypto.JWK` by hand from its public key — mirroring `internal/oauthserver/atcrypto_jwk_test.go`'s reverse direction. Replace the helper above with:

```go
package spaces_server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/clientmeta"
)

const testSpaceOwner = syntax.DID("did:plc:owner")

// attestationTestClient generates a P-256 keypair, serves a
// client-metadata.json embedding its public key (as an atcrypto JWK, under
// kid) at a freshly started test server, and returns the resulting client_id
// (usable as both the served URL and the attestation's iss/sub) plus the
// private key for signing test attestations.
func attestationTestClient(t *testing.T, kid string) (clientID string, priv *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	jwk := atcrypto.JWK{
		KeyType: "EC",
		Curve:   "P-256",
		X:       base64.RawURLEncoding.EncodeToString(priv.PublicKey.X.Bytes()),
		Y:       base64.RawURLEncoding.EncodeToString(priv.PublicKey.Y.Bytes()),
		KeyID:   &kid,
	}

	var url string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		require.NoError(t, json.NewEncoder(w).Encode(oauth.ClientMetadata{
			ClientID: url,
			JWKS:     &oauth.JWKS{Keys: []atcrypto.JWK{jwk}},
		}))
	}))
	t.Cleanup(server.Close)
	url = server.URL + "/client-metadata.json"
	return url, priv
}

// signAttestation builds and signs a client attestation JWT for clientID,
// overriding fields via mutate for negative-path tests.
func signAttestation(
	t *testing.T,
	priv *ecdsa.PrivateKey,
	kid string,
	clientID string,
	spaceOwner syntax.DID,
	mutate func(*josejwt.Claims, map[jose.HeaderKey]any),
) string {
	t.Helper()
	extra := map[jose.HeaderKey]any{jose.HeaderType: attestationTyp}
	claims := &josejwt.Claims{
		Issuer:   clientID,
		Subject:  clientID,
		Audience: josejwt.Audience{spaceOwner.String() + "#atproto_space_host"},
		IssuedAt: josejwt.NewNumericDate(time.Now()),
		Expiry:   josejwt.NewNumericDate(time.Now().Add(30 * time.Second)),
		ID:       "test-nonce",
	}
	if mutate != nil {
		mutate(claims, extra)
	}
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.ES256, Key: priv},
		&jose.SignerOptions{ExtraHeaders: extra}.WithHeader("kid", kid),
	)
	require.NoError(t, err)
	raw, err := josejwt.Signed(signer).Claims(claims).CompactSerialize()
	require.NoError(t, err)
	return raw
}

func TestVerifyAttestationValid(t *testing.T) {
	clientID, priv := attestationTestClient(t, "key-1")
	raw := signAttestation(t, priv, "key-1", clientID, testSpaceOwner, nil)

	got, err := verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
	require.NoError(t, err)
	require.Equal(t, clientID, got)
}

func TestVerifyAttestationRejects(t *testing.T) {
	cases := map[string]func(*josejwt.Claims, map[jose.HeaderKey]any){
		"wrong typ": func(_ *josejwt.Claims, extra map[jose.HeaderKey]any) {
			extra[jose.HeaderType] = "jwt"
		},
		"iss != sub": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.Subject = "https://other.example.com/client-metadata.json"
		},
		"wrong aud": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.Audience = josejwt.Audience{"did:plc:someone-else#atproto_space_host"}
		},
		"expired": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.IssuedAt = josejwt.NewNumericDate(time.Now().Add(-time.Hour))
			c.Expiry = josejwt.NewNumericDate(time.Now().Add(-time.Minute))
		},
		"missing jti": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.ID = ""
		},
		"exp far beyond max ttl": func(c *josejwt.Claims, _ map[jose.HeaderKey]any) {
			c.Expiry = josejwt.NewNumericDate(time.Now().Add(24 * time.Hour))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			clientID, priv := attestationTestClient(t, "key-1")
			raw := signAttestation(t, priv, "key-1", clientID, testSpaceOwner, mutate)

			_, err := verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
			require.ErrorIs(t, err, ErrInvalidAttestation)
		})
	}
}

func TestVerifyAttestationRejectsBadSignature(t *testing.T) {
	clientID, _ := attestationTestClient(t, "key-1")
	otherPriv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	// Signed by a key that doesn't match the one published at clientID.
	raw := signAttestation(t, otherPriv, "key-1", clientID, testSpaceOwner, nil)

	_, err = verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
	require.ErrorIs(t, err, ErrInvalidAttestation)
}

func TestVerifyAttestationRejectsUnknownKid(t *testing.T) {
	clientID, priv := attestationTestClient(t, "key-1")
	raw := signAttestation(t, priv, "key-2", clientID, testSpaceOwner, nil)

	_, err := verifyAttestation(context.Background(), clientmeta.NewResolver(), raw, testSpaceOwner)
	require.ErrorIs(t, err, ErrInvalidAttestation)
}
```

Note: `&jose.SignerOptions{ExtraHeaders: extra}.WithHeader("kid", kid)` is invalid Go (can't call a method on a composite literal directly as written). Fix while implementing:

```go
so := &jose.SignerOptions{ExtraHeaders: extra}
so.WithHeader("kid", kid)
signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: priv}, so)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/spaces/server/... -run TestVerifyAttestation -v`
Expected: FAIL (compile error — `verifyAttestation`, `ErrInvalidAttestation`, `attestationTyp` undefined).

- [ ] **Step 3: Implement `verifyAttestation`**

Create `internal/spaces/server/attestation.go`:

```go
package spaces_server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/go-jose/go-jose/v3/jwt"

	"github.com/habitat-network/habitat/internal/clientmeta"
)

// attestationTyp is the required "typ" header on a client attestation JWT.
// See https://github.com/bluesky-social/proposals/blob/main/0016-permissioned-data/README.md#client-attestation.
const attestationTyp = "atproto-client-attestation+jwt"

// maxAttestationTTL bounds how long-lived an attestation's exp-iat window
// may be. The proposal's example attestations are ~60s; this leaves headroom
// for clock skew between the client and Habitat without accepting
// anomalously long-lived tokens.
const maxAttestationTTL = 5 * time.Minute

// ErrInvalidAttestation wraps every reason an attestation JWT is rejected:
// malformed, badly signed, or failing a claims check.
var ErrInvalidAttestation = errors.New("invalid client attestation")

// verifyAttestation verifies a client attestation JWT presented to
// getSpaceCredential and returns the verified client_id (the attestation's
// iss) on success.
func verifyAttestation(
	ctx context.Context,
	resolver *clientmeta.Resolver,
	raw string,
	spaceOwner syntax.DID,
) (string, error) {
	parsed, err := jwt.ParseSigned(raw)
	if err != nil {
		return "", fmt.Errorf("%w: parse: %v", ErrInvalidAttestation, err)
	}
	if len(parsed.Headers) != 1 {
		return "", fmt.Errorf("%w: expected exactly one signature", ErrInvalidAttestation)
	}
	header := parsed.Headers[0]
	if header.Algorithm != string(jose.ES256) {
		return "", fmt.Errorf("%w: unsupported alg %q", ErrInvalidAttestation, header.Algorithm)
	}
	typ, _ := header.ExtraHeaders[jose.HeaderType].(string)
	if typ != attestationTyp {
		return "", fmt.Errorf("%w: unexpected typ %q", ErrInvalidAttestation, typ)
	}
	if header.KeyID == "" {
		return "", fmt.Errorf("%w: missing kid", ErrInvalidAttestation)
	}

	// The iss claim names the client_id to resolve *before* the signature is
	// verified, which is inherent to this scheme (the verification key lives
	// at a location the token itself names). This is safe: an attacker who
	// doesn't hold the private key for a client_id's published JWKS cannot
	// produce a signature the next step accepts, regardless of what claims
	// they put in an unsigned-so-far token.
	var unverified jwt.Claims
	if err := parsed.UnsafeClaimsWithoutVerification(&unverified); err != nil {
		return "", fmt.Errorf("%w: read claims: %v", ErrInvalidAttestation, err)
	}
	if unverified.Issuer == "" {
		return "", fmt.Errorf("%w: missing iss", ErrInvalidAttestation)
	}

	key, err := resolver.ResolveKey(ctx, unverified.Issuer, header.KeyID)
	if errors.Is(err, clientmeta.ErrKeyNotFound) {
		return "", fmt.Errorf("%w: key not found: %v", ErrInvalidAttestation, err)
	} else if err != nil {
		return "", fmt.Errorf("resolve attestation key: %w", err)
	}

	var claims jwt.Claims
	if err := parsed.Claims(key.Key, &claims); err != nil {
		return "", fmt.Errorf("%w: bad signature: %v", ErrInvalidAttestation, err)
	}

	if claims.Issuer == "" || claims.Issuer != claims.Subject {
		return "", fmt.Errorf("%w: iss must equal sub", ErrInvalidAttestation)
	}
	wantAud := spaceOwner.String() + "#atproto_space_host"
	if !claims.Audience.Contains(wantAud) {
		return "", fmt.Errorf("%w: aud does not match %q", ErrInvalidAttestation, wantAud)
	}
	if claims.ID == "" {
		return "", fmt.Errorf("%w: missing jti", ErrInvalidAttestation)
	}
	if claims.Expiry == nil || claims.IssuedAt == nil {
		return "", fmt.Errorf("%w: missing iat/exp", ErrInvalidAttestation)
	}
	now := time.Now()
	if claims.Expiry.Time().Before(now) {
		return "", fmt.Errorf("%w: expired", ErrInvalidAttestation)
	}
	if claims.Expiry.Time().Before(claims.IssuedAt.Time()) {
		return "", fmt.Errorf("%w: exp before iat", ErrInvalidAttestation)
	}
	if claims.Expiry.Time().Sub(claims.IssuedAt.Time()) > maxAttestationTTL {
		return "", fmt.Errorf("%w: exp too far from iat", ErrInvalidAttestation)
	}

	return claims.Issuer, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/spaces/server/... -run TestVerifyAttestation -v`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/spaces/server/attestation.go internal/spaces/server/attestation_test.go
git commit -m "$(cat <<'EOF'
Add client attestation JWT verification

Verifies the atproto-client-attestation+jwt structure against a
client's published JWKS (resolved via internal/clientmeta): ES256
signature, iss==sub, aud pinned to the space host, and a bounded
iat/exp window. Not yet wired into any endpoint.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JGjST45wbdpQ471zfarFBS
EOF
)"
```

---

### Task 4: `AddAppAccess` / `RemoveAppAccess` handlers + routes

**Files:**
- Create: `internal/spaces/server/app_access.go`
- Create: `internal/spaces/server/app_access_test.go`
- Modify: `internal/spaces/server/server.go` (`Server` struct + `NewServer` gain a `*clientmeta.Resolver` field — needed by Task 5, added here so both tasks' handlers share one field; if you're executing tasks out of order, add it in whichever of Task 4/5 lands first)
- Modify: `internal/spaces/server/server_test.go` (`newTestServerWithOpts` passes a resolver)
- Modify: `cmd/pear/main.go` (register the two new routes, pass a resolver into `spaces_server.NewServer`)

**Interfaces:**
- Consumes: `habitat_syntax.AppAccessCollection`, `habitat_syntax.SpaceRoleManager` (Task 1); `habitat.NetworkHabitatSpaceAddAppAccessInput/Output`, `NetworkHabitatSpaceRemoveAppAccessInput/Output` (Task 1 generated code); `spaces.MarshalRecord`, `store.PutRecord`, `store.DeleteRecord` (existing).
- Produces: `func appAccessRkey(clientID string) (syntax.RecordKey, error)` and handlers `Server.AddAppAccess`, `Server.RemoveAppAccess` — both consumed by Task 5 (`appAccessRkey`) and by route wiring.

- [ ] **Step 1: Read `NewServer` and its call sites**

Read `internal/spaces/server/server.go` (constructor, already shown above) and every call site of `spaces_server.NewServer` (`cmd/pear/main.go`, `internal/spaces/server/server_test.go`'s `newTestServerWithOpts`) before editing, since the constructor signature changes here.

- [ ] **Step 2: Write the failing tests**

Create `internal/spaces/server/app_access_test.go`:

```go
package spaces_server_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	authntest "github.com/habitat-network/habitat/internal/authn/testutil"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

func TestServer_AddAndRemoveAppAccess(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	const clientID = "https://app.example.com/client-metadata.json"

	var addOut habitat.NetworkHabitatSpaceAddAppAccessOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{Space: uri.String(), ClientId: clientID},
		&addOut,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, addOut.Uri)

	records, err := store.ListRecords(
		t.Context(), uri, owner, (*syntax_NSID_ptr)(&habitat_syntax.AppAccessCollection),
	)
	require.NoError(t, err)
	require.Len(t, records, 1)

	var removeOut struct{}
	code = httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.RemoveAppAccess,
		habitat.NetworkHabitatSpaceRemoveAppAccessInput{Space: uri.String(), ClientId: clientID},
		&removeOut,
	)
	require.Equal(t, http.StatusOK, code)

	records, err = store.ListRecords(
		t.Context(), uri, owner, (*syntax_NSID_ptr)(&habitat_syntax.AppAccessCollection),
	)
	require.NoError(t, err)
	require.Empty(t, records)
}

func TestServer_AddAppAccess_Unauthorized(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store, WithValidator(authntest.NewFailureValidator()))
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{
			Space: uri.String(), ClientId: "https://app.example.com/client-metadata.json",
		},
		&apiErr,
	)
	require.Equal(t, http.StatusUnauthorized, code)
}

func TestServer_AddAppAccess_SpaceNotFound(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri := habitat_syntax.ConstructSpaceURI(owner, groupType, "nonexistent")

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{
			Space: uri.String(), ClientId: "https://app.example.com/client-metadata.json",
		},
		&apiErr,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "SpaceNotFound", apiErr.Name)
}

func TestServer_AddAppAccess_InvalidClientId(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	var apiErr atclient.ErrorBody
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{Space: uri.String(), ClientId: "not a url"},
		&apiErr,
	)
	require.Equal(t, http.StatusBadRequest, code)
	require.Equal(t, "InvalidClientId", apiErr.Name)
}
```

The `(*syntax_NSID_ptr)(&habitat_syntax.AppAccessCollection)` cast above is a placeholder for "a `*syntax.NSID` pointing at the constant" — fix it while implementing by assigning to a local variable first, matching the existing test style seen in `TestServer_ListRecords` (read that test in `internal/spaces/server/server_test.go` before writing this one, and copy its exact `collection := habitat_syntax.AppAccessCollection; ...&collection` pattern).

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/spaces/server/... -run TestServer_AddAppAccess -v`
Expected: FAIL (compile error — `s.AddAppAccess` undefined).

- [ ] **Step 4: Add the `clientmeta.Resolver` dependency to `Server`**

Edit `internal/spaces/server/server.go`:

```go
type Server struct {
	store      spaces.Store
	validator  authn.RequestValidator
	decoder    *schema.Decoder
	hive       hive.Hive
	blobs      spaces.BlobStore
	hostKey    atcrypto.PrivateKey
	clientMeta *clientmeta.Resolver
}

// NewServer constructs the spaces server. hostPrivateKey signs delegation
// tokens and space credentials for authors hive does not manage (the store
// holds its own commit-signing authority for repo-head commits). blobs backs
// the uploadBlob and getBlob endpoints. clientMeta resolves OAuth client
// metadata/JWKS for verifying client attestations on getSpaceCredential.
func NewServer(
	store spaces.Store,
	validator authn.RequestValidator,
	hostPrivateKey atcrypto.PrivateKey,
	hive hive.Hive,
	blobs spaces.BlobStore,
	clientMeta *clientmeta.Resolver,
) *Server {
	return &Server{
		store:      store,
		decoder:    schema.NewDecoder(),
		hive:       hive,
		blobs:      blobs,
		hostKey:    hostPrivateKey,
		validator:  validator,
		clientMeta: clientMeta,
	}
}
```

Add `"github.com/habitat-network/habitat/internal/clientmeta"` to the import block.

- [ ] **Step 5: Update `NewServer` call sites**

In `internal/spaces/server/server_test.go`'s `newTestServerWithOpts`, add `clientmeta.NewResolver()` as the final argument to `spaces_server.NewServer(...)`, and add the matching import.

In `cmd/pear/main.go`, find the `spaces_server.NewServer(...)` (or `spacesServer :=` / equivalent) call and add `clientmeta.NewResolver()` as the final argument; add the import. (Grep first: `grep -n "spaces_server.NewServer\|spacesServer :=" cmd/pear/main.go`.)

- [ ] **Step 6: Implement `appAccessRkey` and the handlers**

Create `internal/spaces/server/app_access.go`:

```go
package spaces_server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/habitat"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/spaces"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// appAccessRkey deterministically derives the network.habitat.space.appAccess
// record key for a client_id: its base64url (no padding) encoding, so the
// grant for a given client is directly addressable without a lookup index.
func appAccessRkey(clientID string) (syntax.RecordKey, error) {
	encoded := base64.RawURLEncoding.EncodeToString([]byte(clientID))
	rkey, err := syntax.ParseRecordKey(encoded)
	if err != nil {
		return "", fmt.Errorf("client id too long to address as a record key: %w", err)
	}
	return rkey, nil
}

// validateClientID reports whether clientID is well-formed enough to store:
// an absolute URL, matching what getSpaceCredential's attestation
// verification expects a client_id to look like.
func validateClientID(clientID string) error {
	u, err := url.Parse(clientID)
	if err != nil || !u.IsAbs() {
		return fmt.Errorf("client id must be an absolute URL")
	}
	return nil
}

func (s *Server) AddAppAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceAddAppAccessInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	if err := validateClientID(input.ClientId); err != nil {
		httpx.WriteError(ctx, w, "InvalidClientId", err.Error(), http.StatusBadRequest)
		return
	}
	rkey, err := appAccessRkey(input.ClientId)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidClientId", err.Error(), http.StatusBadRequest)
		return
	}
	recordBytes, err := spaces.MarshalRecord(habitat.NetworkHabitatSpaceAppAccess{})
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("marshal app access record: %w", err))
		return
	}
	recordURI, _, err := s.store.PutRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey, recordBytes,
	)
	if errors.Is(err, spaces.ErrSpaceNotFound) {
		httpx.WriteSpaceNotFound(ctx, w, err)
		return
	} else if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("put app access record: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceAddAppAccessOutput{Uri: recordURI.String()})
}

func (s *Server) RemoveAppAccess(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceRemoveAppAccessInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "decode request body", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleManager),
	).Validate(w, r); !ok {
		return
	}
	rkey, err := appAccessRkey(input.ClientId)
	if err != nil {
		httpx.WriteError(ctx, w, "InvalidClientId", err.Error(), http.StatusBadRequest)
		return
	}
	if err := s.store.DeleteRecord(
		ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, string(rkey),
	); err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("delete app access record: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceRemoveAppAccessOutput{})
}
```

Check `habitat.NetworkHabitatSpaceAppAccess` is the exact generated struct name (from Task 1's `appAccess.json`, following the `NetworkHabitatRelationshipSpaceRelation` naming convention seen in `api/habitat/relationship_spaceRelation.go`) — read the generated file to confirm before using it.

- [ ] **Step 7: Fix the test's placeholder cast**

Go back to `app_access_test.go` and replace the `(*syntax_NSID_ptr)(&habitat_syntax.AppAccessCollection)` placeholders with the real pattern copied from `TestServer_ListRecords` in `server_test.go`, e.g.:

```go
collection := habitat_syntax.AppAccessCollection
records, err := store.ListRecords(t.Context(), uri, owner, &collection)
```

- [ ] **Step 8: Run tests to verify they pass**

Run: `go test ./internal/spaces/server/... -run TestServer_AddAppAccess -v`
Run: `go test ./internal/spaces/server/... -run TestServer_AddAndRemoveAppAccess -v`
Expected: all PASS.

- [ ] **Step 9: Register routes**

Read `cmd/pear/main.go` around the existing `network.habitat.space.*` route block (shown in plan-time research, near `spacesServer.GetSpaceCredential`) and add:

```go
	mux.HandleFunc("/xrpc/network.habitat.space.addAppAccess", spacesServer.AddAppAccess)
	mux.HandleFunc("/xrpc/network.habitat.space.removeAppAccess", spacesServer.RemoveAppAccess)
```

immediately after the existing `getSpaceCredential` route registration.

- [ ] **Step 10: Full package build and test**

Run: `go build ./... && go test ./internal/spaces/... ./cmd/pear/... -v`
Expected: build succeeds, all tests PASS. (If `cmd/pear` has no tests, the `go test` for it will report "no test files" — that's fine.)

- [ ] **Step 11: Commit**

```bash
git add internal/spaces/server internal/syntax cmd/pear/main.go
git commit -m "$(cat <<'EOF'
Add addAppAccess/removeAppAccess handlers and routes

Lets space managers/owners grant or revoke an OAuth client's access
to a space by writing/deleting network.habitat.space.appAccess
records in the space owner's repo, keyed by the base64url-encoded
client_id. Manager-role-gated, independent of generic
PutRecord/DeleteRecord.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JGjST45wbdpQ471zfarFBS
EOF
)"
```

---

### Task 5: Wire enforcement into `GetSpaceCredential`

**Files:**
- Modify: `internal/spaces/server/server.go` (`GetSpaceCredential`)
- Create: `internal/spaces/server/get_space_credential_test.go`

**Interfaces:**
- Consumes: `verifyAttestation`, `ErrInvalidAttestation` (Task 3); `appAccessRkey` (Task 4); `habitat_syntax.AppAccessCollection` (Task 1); `s.clientMeta` field (Task 4).
- Produces: nothing further — this is the last task.

- [ ] **Step 1: Read the current handler once more**

Re-read `internal/spaces/server/server.go`'s `GetSpaceCredential` (shown in full during plan-time research) immediately before editing, since line numbers may have shifted from earlier tasks' edits to the same file (the `Server` struct/`NewServer` change in Task 4).

- [ ] **Step 2: Write the failing tests**

`internal/httpx/testutil.TestXRPCClient` (read in full: it wraps `httptest.NewRecorder`/`httptest.NewRequest` and never sets an Authorization header), and `internal/authn/testutil.validatorImpl.Validate` (read in full: it ignores every `ValidatorMethod`/`WithSpace` option passed to `Request(...)` and always returns the fixed `credentialInfo` when `success: true`) together mean no real delegation token or auth header is needed in these tests — exactly like every other handler test in `server_test.go` (`TestServer_PutAndGetRecord`, `TestServer_GetRepo`, etc.), calling `s.GetSpaceCredential` directly with the default `authntest.NewSuccessValidatorWithOrg(owner, orgID)` validator is sufficient; the delegation-token check passes regardless of what's in `input`/headers.

Create `internal/spaces/server/get_space_credential_test.go`:

```go
package spaces_server_test

import (
	"net/http"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atclient"
	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
)

func TestServer_GetSpaceCredential_OpenSpaceNoAttestation(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.GetSpaceCredential,
		habitat.NetworkHabitatSpaceGetSpaceCredentialInput{Space: uri.String()},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Credential)
}

func TestServer_GetSpaceCredential_AllowListSpace(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	// GetSpaceCredential verifies the attestation's aud against the space
	// owner DID, which orgID's CreateSpace call above makes owner==orgID.
	clientID, priv := attestationTestClient(t, "key-1")

	var addOut habitat.NetworkHabitatSpaceAddAppAccessOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.AddAppAccess,
		habitat.NetworkHabitatSpaceAddAppAccessInput{Space: uri.String(), ClientId: clientID},
		&addOut,
	)
	require.Equal(t, http.StatusOK, code)

	t.Run("no attestation is rejected", func(t *testing.T) {
		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{Space: uri.String()},
			&apiErr,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "InvalidClientAttestation", apiErr.Name)
	})

	t.Run("granted client with valid attestation is accepted", func(t *testing.T) {
		attestation := signAttestation(t, priv, "key-1", clientID, orgID, nil)
		var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), ClientAttestation: attestation,
			},
			&out,
		)
		require.Equal(t, http.StatusOK, code)
		require.NotEmpty(t, out.Credential)
	})

	t.Run("non-granted client is rejected", func(t *testing.T) {
		otherClientID, otherPriv := attestationTestClient(t, "key-1")
		attestation := signAttestation(t, otherPriv, "key-1", otherClientID, orgID, nil)
		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), ClientAttestation: attestation,
			},
			&apiErr,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "AppNotAuthorized", apiErr.Name)
	})

	t.Run("invalid attestation is rejected", func(t *testing.T) {
		var apiErr atclient.ErrorBody
		code := httpx_testutil.NewTestXRPCClient(t).Procedure(
			s.GetSpaceCredential,
			habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
				Space: uri.String(), ClientAttestation: "not-a-jwt",
			},
			&apiErr,
		)
		require.Equal(t, http.StatusBadRequest, code)
		require.Equal(t, "InvalidClientAttestation", apiErr.Name)
	})
}

func TestServer_GetSpaceCredential_OpenSpaceWithValidAttestation(t *testing.T) {
	key, store := newTestStore(t)
	s := newTestServerWithOpts(t, key, store)
	uri, err := store.CreateSpace(t.Context(), orgID, groupType, "test")
	require.NoError(t, err)

	clientID, priv := attestationTestClient(t, "key-1")
	attestation := signAttestation(t, priv, "key-1", clientID, orgID, nil)

	var out habitat.NetworkHabitatSpaceGetSpaceCredentialOutput
	code := httpx_testutil.NewTestXRPCClient(t).Procedure(
		s.GetSpaceCredential,
		habitat.NetworkHabitatSpaceGetSpaceCredentialInput{
			Space: uri.String(), ClientAttestation: attestation,
		},
		&out,
	)
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, out.Credential)
}
```

Note `orgID` (not `owner`) is used as the attestation `aud`/`spaceOwner` throughout: `newTestStore`'s spaces are created with `store.CreateSpace(t.Context(), orgID, groupType, "test")`, so `spaceURI.SpaceOwner() == orgID` — verify this matches `TestServer_AddAndRemoveAppAccess` (Task 4), which uses the same `orgID`-owned space, before running these.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/spaces/server/... -run TestServer_GetSpaceCredential -v`
Expected: FAIL — either compile errors (before Step 5's implementation) or, if it compiles against the old handler, `TestServer_GetSpaceCredential_AllowListSpace` fails because every request still hits the `NotSupported` stub / never checks the allow-list.

- [ ] **Step 4: Implement the enforcement change**

Edit `internal/spaces/server/server.go`'s `GetSpaceCredential`:

```go
func (s *Server) GetSpaceCredential(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var input habitat.NetworkHabitatSpaceGetSpaceCredentialInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		httpx.WriteInvalidRequest(ctx, w, "failed to parse params", err)
		return
	}
	spaceURI, ok := httpx.ParseSpaceURIInput(ctx, w, input.Space, "space uri")
	if !ok {
		return
	}
	if _, ok = s.validator.Request(
		authn.WithMethods(authn.ValidatorMethodDelegationToken),
		authn.WithSpace(spaceURI, habitat_syntax.SpaceRoleReader),
	).Validate(w, r); !ok {
		return
	}

	collection := habitat_syntax.AppAccessCollection
	grants, err := s.store.ListRecords(ctx, spaceURI, spaceURI.SpaceOwner(), &collection)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("list app access grants: %w", err))
		return
	}
	allowListed := len(grants) > 0

	if input.ClientAttestation == "" {
		if allowListed {
			httpx.WriteError(
				ctx, w, "InvalidClientAttestation",
				"space requires a client attestation", http.StatusBadRequest,
			)
			return
		}
	} else {
		clientID, err := verifyAttestation(ctx, s.clientMeta, input.ClientAttestation, spaceURI.SpaceOwner())
		if errors.Is(err, ErrInvalidAttestation) {
			httpx.WriteError(ctx, w, "InvalidClientAttestation", err.Error(), http.StatusBadRequest)
			return
		} else if err != nil {
			httpx.WriteServerError(ctx, w, fmt.Errorf("verify attestation: %w", err))
			return
		}
		if allowListed {
			rkey, err := appAccessRkey(clientID)
			if err != nil {
				httpx.WriteError(ctx, w, "InvalidClientAttestation", err.Error(), http.StatusBadRequest)
				return
			}
			if _, err := s.store.GetRecord(
				ctx, spaceURI, spaceURI.SpaceOwner(), habitat_syntax.AppAccessCollection, rkey,
			); errors.Is(err, spaces.ErrRecordNotFound) {
				httpx.WriteError(ctx, w, "AppNotAuthorized", "", http.StatusBadRequest)
				return
			} else if err != nil {
				httpx.WriteServerError(ctx, w, fmt.Errorf("check app access grant: %w", err))
				return
			}
		}
	}

	kid := "#atproto"
	privKey, err := s.hive.PrivateKeyForDID(ctx, spaceURI.SpaceOwner())
	if errors.Is(err, identity.ErrDIDNotFound) {
		privKey = s.hostKey
		kid = "#atproto_space"
	} else if err != nil {
		httpx.WriteSpaceNotFound(ctx, w, fmt.Errorf("failed to get host private key: %w", err))
		return
	}
	token, err := utils.SpaceCredential(privKey, kid, spaceURI)
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("failed to sign token: %w", err))
		return
	}
	httpx.WriteJSON(ctx, w, habitat.NetworkHabitatSpaceGetSpaceCredentialOutput{Credential: token})
}
```

This removes the `if input.ClientAttestation != "" { httpx.WriteNotSupported(...) }` stub entirely.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/spaces/server/... -run "TestServer_GetSpaceCredential|TestServer_AddAppAccess|TestServer_AddAndRemoveAppAccess|TestVerifyAttestation" -v`
Expected: all PASS.

- [ ] **Step 6: Run the full package and module test suite**

Run: `go test ./internal/... -v 2>&1 | tail -100`
Run: `go build ./...`
Expected: no failures, no build errors. Pay particular attention to `internal/oauthserver` and `internal/spaces` in the output, since both were touched across this plan.

- [ ] **Step 7: Lint**

Run: `golangci-lint run ./internal/clientmeta/... ./internal/spaces/... ./internal/oauthserver/... ./internal/syntax/...`
Expected: no new findings. Fix anything flagged (in particular `bodyclose` on the new HTTP calls in `internal/clientmeta` — every `resp.Body` must be closed, which the drafted code already does via `defer func() { _ = resp.Body.Close() }()`, matching the existing style in `fosite_storage.go`).

- [ ] **Step 8: Commit**

```bash
git add internal/spaces/server/server.go internal/spaces/server/get_space_credential_test.go
git commit -m "$(cat <<'EOF'
Enforce client attestation on getSpaceCredential

Closes the NotSupported stub: a space with at least one appAccess
grant now requires and verifies a client attestation, checking the
verified client_id against its grants; an open space accepts a
present-but-optional attestation and never requires one.

Co-Authored-By: Claude Sonnet 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01JGjST45wbdpQ471zfarFBS
EOF
)"
```

---

## Plan-level self-review notes

- **Spec coverage:** Data model → Tasks 1, 4. Attestation JWT verification (shared resolver + attestation-specific checks) → Tasks 2, 3. `GetSpaceCredential` changes → Task 5. Testing section's four bullet groups → distributed across Tasks 2 (clientmeta), 3 (attestation), 4 (grant lifecycle), 5 (end-to-end). No spec section is without a task.
- **Type/name consistency check:** `verifyAttestation` (Task 3) is called with the same signature `(ctx, resolver, raw, spaceOwner)` in Task 5. `appAccessRkey` (Task 4) is reused as-is in Task 5. `habitat_syntax.AppAccessCollection` (Task 1) is used identically in Tasks 4 and 5. `clientmeta.Resolver`/`ResolveKey`/`ErrKeyNotFound`/`ConvertJWK` (Task 2) are the exact names Task 3 imports.
- **Known soft spot flagged inline:** Task 5 Step 2 explicitly calls out that the delegation-token-bearer test plumbing is unverified against the real `httpx_testutil` API and must be resolved by reading the code at execution time, not assumed — this is the one place the plan couldn't fully pin down a exact test helper signature during research.
