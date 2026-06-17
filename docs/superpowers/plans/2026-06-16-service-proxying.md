# Service Proxying Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement AT Protocol service proxying in pear so clients can forward XRPC requests to third-party services (labelers, AppViews) via the `Atproto-Proxy` header.

**Architecture:** A gorilla/mux middleware (`ServiceProxy`) intercepts every request that carries an `Atproto-Proxy: <did>#<serviceId>` header before any handler fires. It validates the caller's OAuth session, resolves the target DID via `identity.Directory`, signs a service auth JWT on behalf of the caller via `hive.Hive.SignServiceAuth`, and forwards the request. Requests without the header pass through to existing handlers (including `PDSForwarding`) unchanged.

**Tech Stack:** Go stdlib (`net/http`, `net/url`, `io`), `github.com/bluesky-social/indigo/atproto/identity`, `github.com/bluesky-social/indigo/atproto/syntax`, internal `authn`, `hive`, `utils` packages, `gorm.io/driver/sqlite` (integration test only), `testify/require`.

---

## File Map

| File | Role |
|------|------|
| `internal/forwarding/service_proxy.go` | New — `ServiceProxy` type, `NewServiceProxy` constructor, `Middleware` method |
| `internal/forwarding/service_proxy_test.go` | New — unit tests (nil hive for cases that don't reach signing) + one integration test with real in-memory hive |
| `cmd/pear/main.go` | Construct `ServiceProxy` after `oauthServer`; add `mux.Use(serviceProxy.Middleware)` |

---

### Task 1: Full ServiceProxy implementation

**Files:**
- Create: `internal/forwarding/service_proxy.go`

- [ ] **Step 1: Create `service_proxy.go`**

```go
package forwarding

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/habitat-network/habitat/internal/utils"
)

// hopByHopHeaders are stripped before forwarding per the HTTP/1.1 spec.
var hopByHopHeaders = []string{
	"Connection", "Transfer-Encoding", "Te", "Upgrade", "Keep-Alive",
}

// ServiceProxy implements AT Protocol service proxying: when an incoming XRPC
// request carries an Atproto-Proxy header, it validates the caller's OAuth
// session and forwards the request to the specified service using a service
// auth JWT signed on the caller's behalf.
type ServiceProxy struct {
	oauth authn.Method
	h     hive.Hive
	dir   identity.Directory
}

// NewServiceProxy constructs a ServiceProxy.
// oauth validates the incoming caller's session.
// h signs service auth JWTs for forwarded requests via hive.SignServiceAuth.
// dir resolves external DIDs to find service endpoints.
func NewServiceProxy(oauth authn.Method, h hive.Hive, dir identity.Directory) *ServiceProxy {
	return &ServiceProxy{
		oauth: oauth,
		h:     h,
		dir:   dir,
	}
}

// Middleware returns a gorilla/mux-compatible middleware handler. Requests
// carrying an Atproto-Proxy header on an XRPC path are intercepted and
// forwarded to the specified service; all other requests pass through unchanged.
func (s *ServiceProxy) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyHeader := r.Header.Get("Atproto-Proxy")
		if proxyHeader == "" || !strings.HasPrefix(r.URL.Path, "/xrpc/") {
			next.ServeHTTP(w, r)
			return
		}
		s.proxy(w, r, proxyHeader)
	})
}

func (s *ServiceProxy) proxy(w http.ResponseWriter, r *http.Request, proxyHeader string) {
	// Parse "did#serviceId" — the "#" separator is required by the AT Protocol spec.
	rawDID, serviceID, ok := strings.Cut(proxyHeader, "#")
	if !ok {
		utils.WriteHTTPError(w, fmt.Errorf("malformed Atproto-Proxy header: missing '#'"), http.StatusBadRequest)
		return
	}

	// Validate the caller's OAuth session before acting on their behalf.
	callerDID, ok := authn.Validate(w, r, s.oauth)
	if !ok {
		return
	}

	targetDID, err := syntax.ParseDID(rawDID)
	if err != nil {
		utils.WriteHTTPError(w, fmt.Errorf("invalid DID in Atproto-Proxy header: %w", err), http.StatusBadRequest)
		return
	}

	// Use context.Background() to avoid cached context cancelled errors:
	// https://github.com/bluesky-social/indigo/pull/1345
	id, err := s.dir.LookupDID(context.Background(), targetDID)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to resolve proxy target DID", http.StatusBadGateway)
		return
	}

	svc, ok := id.Services[serviceID]
	if !ok {
		utils.WriteHTTPError(w, fmt.Errorf("service %q not found in DID document for %s", serviceID, targetDID), http.StatusBadRequest)
		return
	}

	nsidStr := strings.TrimPrefix(r.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		utils.WriteHTTPError(w, fmt.Errorf("invalid NSID in path: %w", err), http.StatusBadRequest)
		return
	}

	// Habitat owns user signing keys, so it signs service auth tokens on behalf
	// of users — the same role a PDS fills when calling com.atproto.server.getServiceAuth.
	jwt, err := s.h.SignServiceAuth(r.Context(), callerDID, targetDID.String(), 60*time.Second, &nsid)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to sign service auth", http.StatusInternalServerError)
		return
	}

	base, err := url.Parse(svc.URL)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: invalid service URL in DID document", http.StatusBadGateway)
		return
	}
	requestURI, err := url.Parse(r.URL.RequestURI())
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to parse request URI", http.StatusInternalServerError)
		return
	}
	forwardURL := base.ResolveReference(requestURI)

	var body io.Reader
	if r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch {
		body = r.Body
	}

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, forwardURL.String(), body)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: failed to build forwarded request", http.StatusInternalServerError)
		return
	}

	// Clone headers, then scrub hop-by-hop and override auth.
	outReq.Header = r.Header.Clone()
	for _, h := range hopByHopHeaders {
		outReq.Header.Del(h)
	}
	outReq.Header.Set("Authorization", "Bearer "+jwt)
	// Strip Atproto-Proxy to prevent the target from attempting further proxying.
	outReq.Header.Del("Atproto-Proxy")

	resp, err := http.DefaultClient.Do(outReq)
	if err != nil {
		utils.LogAndHTTPError(r.Context(), w, err, "[service proxy]: forwarded request failed", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for key, values := range resp.Header {
		for _, value := range values {
			w.Header().Set(key, value)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		if utils.ShouldLog(err) {
			slog.ErrorContext(r.Context(), "[service proxy]: failed to copy response body", "err", err)
		}
	}
}
```

- [ ] **Step 2: Build to confirm no compilation errors**

```bash
moon pear:build
```

Expected: build succeeds.

- [ ] **Step 3: Commit**

```bash
git add internal/forwarding/service_proxy.go
git commit -m "feat: implement ServiceProxy middleware"
```

---

### Task 2: Tests

**Files:**
- Create: `internal/forwarding/service_proxy_test.go`

Unit tests pass `nil` for the hive — all cases fail before reaching `s.h.SignServiceAuth`. The final integration test uses a real in-memory hive (SQLite) to exercise the full signing and forwarding path.

- [ ] **Step 1: Create `service_proxy_test.go`**

```go
package forwarding

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/hive"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestServiceProxy_NoHeader_CallsNext(t *testing.T) {
	sp := NewServiceProxy(authn.NewStubAuthnForTest("did:web:caller.example"), nil, identity.NewMockDirectory())

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	rec := httptest.NewRecorder()
	sp.Middleware(next).ServeHTTP(rec, req)

	require.True(t, nextCalled)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestServiceProxy_MalformedHeader_Returns400(t *testing.T) {
	callerDID, err := syntax.ParseDID("did:web:caller.example")
	require.NoError(t, err)

	sp := NewServiceProxy(authn.NewStubAuthnForTest(callerDID), nil, identity.NewMockDirectory())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	req.Header.Set("Atproto-Proxy", "did:web:target.example") // missing '#serviceId'
	rec := httptest.NewRecorder()

	sp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called")
	})).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestServiceProxy_AuthFails_Returns401(t *testing.T) {
	sp := NewServiceProxy(authn.NewStubAuthnFailedForTest(), nil, identity.NewMockDirectory())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	req.Header.Set("Atproto-Proxy", "did:web:target.example#atproto_labeler")
	rec := httptest.NewRecorder()

	sp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called")
	})).ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServiceProxy_DIDResolutionFails_Returns502(t *testing.T) {
	callerDID, err := syntax.ParseDID("did:web:caller.example")
	require.NoError(t, err)

	// Empty MockDirectory returns ErrDIDNotFound for any lookup.
	sp := NewServiceProxy(authn.NewStubAuthnForTest(callerDID), nil, identity.NewMockDirectory())

	req := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	req.Header.Set("Atproto-Proxy", "did:web:target.example#atproto_labeler")
	rec := httptest.NewRecorder()

	sp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called")
	})).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestServiceProxy_ServiceNotFound_Returns400(t *testing.T) {
	callerDID, err := syntax.ParseDID("did:web:caller.example")
	require.NoError(t, err)
	targetDID, err := syntax.ParseDID("did:web:target.example")
	require.NoError(t, err)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID:      targetDID,
		Services: map[string]identity.ServiceEndpoint{}, // no "atproto_labeler"
	})

	sp := NewServiceProxy(authn.NewStubAuthnForTest(callerDID), nil, dir)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/app.bsky.feed.getTimeline", nil)
	req.Header.Set("Atproto-Proxy", "did:web:target.example#atproto_labeler")
	rec := httptest.NewRecorder()

	sp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called")
	})).ServeHTTP(rec, req)

	require.Equal(t, http.StatusBadRequest, rec.Code)
}

// TestServiceProxy_Integration exercises the full path: OAuth validation,
// service auth signing via a real in-memory hive, and request forwarding.
func TestServiceProxy_Integration_ForwardsWithServiceAuth(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Discard})
	require.NoError(t, err)
	h, err := hive.NewHive("example.com", "pear.example.com", db)
	require.NoError(t, err)

	// Mint a caller identity so the hive has a signing key for it.
	callerID, err := h.MintIdentity(context.Background(), "alice", "org")
	require.NoError(t, err)

	targetDID, err := syntax.ParseDID("did:web:target.example")
	require.NoError(t, err)

	// Fake target service — captures the forwarded request.
	var capturedAuth, capturedProxy string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		capturedProxy = r.Header.Get("Atproto-Proxy")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(target.Close)

	dir := identity.NewMockDirectory()
	dir.Insert(identity.Identity{
		DID: targetDID,
		Services: map[string]identity.ServiceEndpoint{
			"atproto_labeler": {Type: "AtprotoLabeler", URL: target.URL},
		},
	})

	sp := NewServiceProxy(authn.NewStubAuthnForTest(callerID.DID), h, dir)

	req := httptest.NewRequest(http.MethodGet, "/xrpc/com.atproto.label.queryLabels", nil)
	req.Header.Set("Atproto-Proxy", "did:web:target.example#atproto_labeler")
	req.Header.Set("Authorization", "Bearer original-token")
	rec := httptest.NewRecorder()

	sp.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next should not be called when Atproto-Proxy is set")
	})).ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	body, err := io.ReadAll(rec.Body)
	require.NoError(t, err)
	require.JSONEq(t, `{"ok":true}`, string(body))
	// A real service auth JWT must have been forwarded (not the original token).
	require.True(t, strings.HasPrefix(capturedAuth, "Bearer "), "expected Bearer JWT, got: %s", capturedAuth)
	require.NotEqual(t, "Bearer original-token", capturedAuth, "original auth must be replaced by service auth JWT")
	// Atproto-Proxy must be stripped to prevent forwarding loops.
	require.Empty(t, capturedProxy)
}
```

- [ ] **Step 2: Run tests**

```bash
go test ./internal/forwarding/... -v
```

Expected: all tests pass.

- [ ] **Step 3: Commit**

```bash
git add internal/forwarding/service_proxy_test.go
git commit -m "test: add ServiceProxy middleware tests"
```

---

### Task 3: Wire ServiceProxy into cmd/pear/main.go

**Files:**
- Modify: `cmd/pear/main.go`

- [ ] **Step 1: Construct ServiceProxy and register middleware**

In `cmd/pear/main.go`, immediately after the `oauthServer` error check (around line 258), insert:

```go
serviceProxy := forwarding.NewServiceProxy(oauthServer, orgHive, dir)
mux.Use(serviceProxy.Middleware)
```

`oauthServer` satisfies `authn.Method`. `orgHive` is a `hive.Hive`. `dir` is the `identity.Directory` already in scope at line 183.

`mux.Use` applies middleware in registration order. Registering here (after `otelmux` and `corsMiddleware` at lines 165–166) means service proxy runs third — after tracing and CORS, before route dispatch.

- [ ] **Step 2: Build**

```bash
moon pear:build
```

Expected: build succeeds.

- [ ] **Step 3: Run all Go tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add cmd/pear/main.go
git commit -m "feat: wire ServiceProxy middleware into pear server"
```
