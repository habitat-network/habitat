# Service Proxying Design

**Date:** 2026-06-16

## Context

AT Protocol defines a [service proxying](https://atproto.com/specs/xrpc#service-proxying) mechanism: when a client sends an `Atproto-Proxy: <did>#<serviceId>` header, the receiving server should forward the XRPC request to the specified service rather than handling it locally. This lets OAuth-authenticated clients route requests through habitat to third-party AT Protocol services (labelers, AppViews, etc.) without needing direct credentials to those services.

Habitat's pear server already allows `atproto-proxy` as a CORS header, indicating this feature was anticipated. The `hive.Hive` interface already provides `SignServiceAuth` to mint service auth JWTs on behalf of user DIDs — exactly what's needed to authenticate the forwarded request.

## Architecture

### New type: `ServiceProxy`

**File:** `internal/forwarding/service_proxy.go`

```go
type ServiceProxy struct {
    oauth authn.Method
    hive  hive.Hive
    dir   identity.Directory
}

func NewServiceProxy(oauth authn.Method, hive hive.Hive, dir identity.Directory) *ServiceProxy

func (s *ServiceProxy) Middleware(next http.Handler) http.Handler
```

### Wire-up in `cmd/pear/main.go`

Construct `ServiceProxy` after the oauth server and hive are initialized, then register it via `mux.Use`:

```go
serviceProxy := forwarding.NewServiceProxy(oauthServer, orgHive, dir)
mux.Use(serviceProxy.Middleware)  // after corsMiddleware
```

## Request Lifecycle

**If `Atproto-Proxy` header is absent:** middleware calls `next.ServeHTTP` — all existing handlers including `PDSForwarding` behave exactly as today.

**If `Atproto-Proxy` header is present:**

1. Validate caller's OAuth session via `authn.Validate(w, r, s.oauth)` → `callerDID`
2. Parse header value: expected format `<did>#<serviceId>` (e.g. `did:web:bsky.network#atproto_labeler`)
3. Resolve target DID document via `s.dir.LookupDID(ctx, targetDID)`
4. Find service endpoint: `identity.Identity.Services[serviceId].URL`
5. Parse NSID from request path: strip `/xrpc/` prefix
6. Sign service auth JWT: `s.hive.SignServiceAuth(ctx, callerDID, targetDID.String(), 60*time.Second, &nsid)`
7. Build forwarded request to `<serviceEndpoint>/xrpc/<nsid>`:
   - `Authorization: Bearer <jwt>` (replaces original auth)
   - Remove `Atproto-Proxy` header (loop prevention)
   - Strip hop-by-hop headers: Connection, Transfer-Encoding, Te, Upgrade, Keep-Alive
   - Pass remaining headers and body through unchanged
8. Execute request via `http.DefaultClient`
9. Copy response status + headers + body back to client

## Error Handling

| Condition | HTTP Status |
|-----------|-------------|
| Malformed header (missing `#`) | 400 Bad Request |
| Invalid DID in header | 400 Bad Request |
| OAuth session invalid/missing | 401 Unauthorized |
| DID resolution fails | 502 Bad Gateway |
| Service ID not found in DID doc | 400 Bad Request |
| Upstream error response | Proxied through as-is |

## Files

| File | Change |
|------|--------|
| `internal/forwarding/service_proxy.go` | New — `ServiceProxy` type and middleware |
| `internal/forwarding/service_proxy_test.go` | New — unit tests with mocked `authn.Method` and `hive.Hive` |
| `cmd/pear/main.go` | Construct `ServiceProxy`, add `mux.Use(serviceProxy.Middleware)` |

## Comments

Add doc comments on the exported type and constructor. Add inline comments on non-obvious implementation steps in `Middleware`:
- Why `Atproto-Proxy` is stripped on the forwarded request (loop prevention)
- Why `callerDID` is used as the JWT issuer (habitat owns user signing keys, so it signs on behalf of the user — same role as a PDS calling `com.atproto.server.getServiceAuth`)
- Why hop-by-hop headers are stripped before forwarding

## Non-Conflict with PDSForwarding

`PDSForwarding` is registered for `/xrpc/com.atproto.repo.*` and `/xrpc/com.atproto.sync.*`. The new middleware fires before route dispatch. When `Atproto-Proxy` is absent (all current traffic), the middleware is a no-op and `PDSForwarding` handles its routes unchanged.

## Verification

- `go test ./internal/forwarding/...` — unit tests for `ServiceProxy`
- Integration: make a request to any XRPC endpoint with `Atproto-Proxy: did:web:bsky.network#atproto_labeler` and verify the request is forwarded to `https://bsky.network/xrpc/<method>` with a valid service auth JWT
- Verify requests without the header continue to reach their original handlers (PDSForwarding, repo handlers, etc.)
- Verify malformed header values return 400, unauthenticated requests return 401
