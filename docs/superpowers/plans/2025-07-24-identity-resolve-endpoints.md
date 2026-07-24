# com.atproto.identity.* Resolution Endpoints

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `com.atproto.identity.resolveDid`, `com.atproto.identity.resolveHandle`, and `com.atproto.identity.resolveIdentity` XRPC endpoints that resolve AT Protocol identities using hive (local) with fallback to the public AT Protocol directory.

**Architecture:** Lexicon JSON files are added to `lexicons/com/atproto/identity/` for spec compliance. Handler methods are added to the existing `internal/identity/server.go`. The server struct gets a new `directory` field (an `identity.Directory`) that wraps hive + default directory for fallback resolution. Routes are registered in `cmd/pear/main.go`. No code generation is needed since `com.atproto.*` lexicons are not in the lexgen config — types are defined inline in the handler file.

**Tech Stack:** Go, gorilla/mux, indigo identity/syntax packages, hive

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Create | `lexicons/com/atproto/identity/defs.json` | IdentityInfo object type definition |
| Create | `lexicons/com/atproto/identity/resolveDid.json` | resolveDid lexicon schema |
| Create | `lexicons/com/atproto/identity/resolveHandle.json` | resolveHandle lexicon schema |
| Create | `lexicons/com/atproto/identity/resolveIdentity.json` | resolveIdentity lexicon schema |
| Modify | `internal/identity/server.go` | Add `directory` field, `NewServer`, and three handler methods |
| Modify | `cmd/pear/main.go` | Register `/xrpc/com.atproto.identity.*` routes |

---

### Task 1: Add lexicon JSON files

**Files:**
- Create: `lexicons/com/atproto/identity/defs.json`
- Create: `lexicons/com/atproto/identity/resolveDid.json`
- Create: `lexicons/com/atproto/identity/resolveHandle.json`
- Create: `lexicons/com/atproto/identity/resolveIdentity.json`

- [ ] **Step 1: Create the identity defs lexicon**

```bash
mkdir -p lexicons/com/atproto/identity
```

Write `lexicons/com/atproto/identity/defs.json`:
```json
{
  "lexicon": 1,
  "id": "com.atproto.identity.defs",
  "defs": {
    "identityInfo": {
      "type": "object",
      "required": ["did", "handle", "didDoc"],
      "properties": {
        "did": { "type": "string", "format": "did" },
        "handle": {
          "type": "string",
          "format": "handle",
          "description": "The validated handle of the account; or 'handle.invalid' if the handle did not bi-directionally match the DID document."
        },
        "didDoc": {
          "type": "unknown",
          "description": "The complete DID document for the identity."
        }
      }
    }
  }
}
```

- [ ] **Step 2: Create the resolveDid lexicon**

Write `lexicons/com/atproto/identity/resolveDid.json`:
```json
{
  "lexicon": 1,
  "id": "com.atproto.identity.resolveDid",
  "defs": {
    "main": {
      "type": "query",
      "description": "Resolves DID to DID document. Does not bi-directionally verify handle.",
      "parameters": {
        "type": "params",
        "required": ["did"],
        "properties": {
          "did": {
            "type": "string",
            "format": "did",
            "description": "DID to resolve."
          }
        }
      },
      "output": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "required": ["didDoc"],
          "properties": {
            "didDoc": {
              "type": "unknown",
              "description": "The complete DID document for the identity."
            }
          }
        }
      },
      "errors": [
        {
          "name": "DidNotFound",
          "description": "The DID resolution process confirmed that there is no current DID."
        },
        {
          "name": "DidDeactivated",
          "description": "The DID previously existed, but has been deactivated."
        }
      ]
    }
  }
}
```

- [ ] **Step 3: Create the resolveHandle lexicon**

Write `lexicons/com/atproto/identity/resolveHandle.json`:
```json
{
  "lexicon": 1,
  "id": "com.atproto.identity.resolveHandle",
  "defs": {
    "main": {
      "type": "query",
      "description": "Resolves an atproto handle (hostname) to a DID. Does not necessarily bi-directionally verify against the DID document.",
      "parameters": {
        "type": "params",
        "required": ["handle"],
        "properties": {
          "handle": {
            "type": "string",
            "format": "handle",
            "description": "The handle to resolve."
          }
        }
      },
      "output": {
        "encoding": "application/json",
        "schema": {
          "type": "object",
          "required": ["did"],
          "properties": {
            "did": { "type": "string", "format": "did" }
          }
        }
      },
      "errors": [
        {
          "name": "HandleNotFound",
          "description": "The resolution process confirmed that the handle does not resolve to any DID."
        }
      ]
    }
  }
}
```

- [ ] **Step 4: Create the resolveIdentity lexicon**

Write `lexicons/com/atproto/identity/resolveIdentity.json`:
```json
{
  "lexicon": 1,
  "id": "com.atproto.identity.resolveIdentity",
  "defs": {
    "main": {
      "type": "query",
      "description": "Resolves an identity (DID or Handle) to a full identity (DID document and verified handle).",
      "parameters": {
        "type": "params",
        "required": ["identifier"],
        "properties": {
          "identifier": {
            "type": "string",
            "format": "at-identifier",
            "description": "Handle or DID to resolve."
          }
        }
      },
      "output": {
        "encoding": "application/json",
        "schema": {
          "type": "ref",
          "ref": "com.atproto.identity.defs#identityInfo"
        }
      },
      "errors": [
        {
          "name": "HandleNotFound",
          "description": "The resolution process confirmed that the handle does not resolve to any DID."
        },
        {
          "name": "DidNotFound",
          "description": "The DID resolution process confirmed that there is no current DID."
        },
        {
          "name": "DidDeactivated",
          "description": "The DID previously existed, but has been deactivated."
        }
      ]
    }
  }
}
```

- [ ] **Step 5: Commit lexicon files**

```bash
git add lexicons/com/atproto/identity/
git commit -m "lexicons: add com.atproto.identity resolve lexicons"
```

---

### Task 2: Update identity server with handler methods

**Files:**
- Modify: `internal/identity/server.go`

- [ ] **Step 1: Add `directory` field and update `NewServer`**

The `Server` struct needs a new `directory` field for fallback identity resolution. Add a `NewServer` constructor that builds a `WrappedDirectory` from hive + the default AT Protocol directory. Update the existing `NewServer` signature to accept all needed dependencies.

The current `NewServer` signature is:
```go
func NewServer(
    hive hive.Hive,
    oauth authn.Method,
    orgStore org.Store,
    pdsForwarding *forwarding.PDSForwarding,
) (*Server, error)
```

Change to:
```go
func NewServer(
    hiveInstance hive.Hive,
    oauth authn.Method,
    orgStore org.Store,
    pdsForwarding *forwarding.PDSForwarding,
) (*Server, error) {
    defaultDir := identity.DefaultDirectory()
    dir := NewWrappedDirectory(hiveInstance, defaultDir)
    return &Server{
        hive:          hiveInstance,
        directory:     dir,
        oauth:         oauth,
        orgStore:      orgStore,
        pdsForwarding: pdsForwarding,
    }, nil
}
```

Update the struct to add the `directory` field:
```go
type Server struct {
    hive          hive.Hive
    directory     identity.Directory
    oauth         authn.Method
    orgStore      org.Store
    pdsForwarding *forwarding.PDSForwarding
}
```

- [ ] **Step 2: Add response types and handler methods**

Add the following types and methods at the end of `internal/identity/server.go`:

```go
// Response types for com.atproto.identity.* endpoints.

type resolveDidOutput struct {
	DIDDoc interface{} `json:"didDoc"`
}

type resolveHandleOutput struct {
	DID string `json:"did"`
}

type identityInfo struct {
	DID    string      `json:"did"`
	Handle string      `json:"handle"`
	DIDDoc interface{} `json:"didDoc"`
}

// ResolveDid implements com.atproto.identity.resolveDid.
func (s *Server) ResolveDid(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	didStr := r.URL.Query().Get("did")
	if didStr == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: did", nil)
		return
	}

	did, err := syntax.ParseDID(didStr)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid did", err)
		return
	}

	ident, err := s.directory.LookupDID(ctx, did)
	if errors.Is(err, identity.ErrDIDNotFound) {
		httpx.WriteError(ctx, w, "DidNotFound", "DID not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving DID: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, resolveDidOutput{
		DIDDoc: ident.DIDDocument(),
	})
}

// ResolveHandle implements com.atproto.identity.resolveHandle.
func (s *Server) ResolveHandle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	handleStr := r.URL.Query().Get("handle")
	if handleStr == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: handle", nil)
		return
	}

	handle, err := syntax.ParseHandle(handleStr)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid handle", err)
		return
	}

	ident, err := s.directory.LookupHandle(ctx, handle)
	if errors.Is(err, identity.ErrHandleNotFound) {
		httpx.WriteError(ctx, w, "HandleNotFound", "handle not found", http.StatusNotFound)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving handle: %w", err))
		return
	}

	httpx.WriteJSON(ctx, w, resolveHandleOutput{
		DID: ident.DID.String(),
	})
}

// ResolveIdentity implements com.atproto.identity.resolveIdentity.
func (s *Server) ResolveIdentity(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	identifier := r.URL.Query().Get("identifier")
	if identifier == "" {
		httpx.WriteInvalidRequest(ctx, w, "missing required parameter: identifier", nil)
		return
	}

	atid, err := syntax.ParseAtIdentifier(identifier)
	if err != nil {
		httpx.WriteInvalidRequest(ctx, w, "invalid identifier", err)
		return
	}

	ident, err := s.directory.Lookup(ctx, atid)
	if errors.Is(err, identity.ErrDIDNotFound) || errors.Is(err, identity.ErrHandleNotFound) {
		errName := "DidNotFound"
		httpStatus := http.StatusNotFound
		if errors.Is(err, identity.ErrHandleNotFound) {
			errName = "HandleNotFound"
		}
		httpx.WriteError(ctx, w, errName, err.Error(), httpStatus)
		return
	}
	if err != nil {
		httpx.WriteServerError(ctx, w, fmt.Errorf("resolving identity: %w", err))
		return
	}

	handle := ident.Handle.String()
	if handle == "" || handle == "handle.invalid" {
		handle = "handle.invalid"
	}

	httpx.WriteJSON(ctx, w, identityInfo{
		DID:    ident.DID.String(),
		Handle: handle,
		DIDDoc: ident.DIDDocument(),
	})
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./internal/identity/
```

Expected: compiles without errors.

- [ ] **Step 4: Commit**

```bash
git add internal/identity/server.go
git commit -m "identity: add ResolveDid, ResolveHandle, ResolveIdentity handlers"
```

---

### Task 3: Register routes in main.go

**Files:**
- Modify: `cmd/pear/main.go`

- [ ] **Step 1: Add route registrations**

After the existing identity server route registrations (around line 530, after `mux.HandleFunc("/xrpc/com.atproto.server.getServiceAuth", idServer.GetServiceAuth)`), add:

```go
mux.HandleFunc("/xrpc/com.atproto.identity.resolveDid", idServer.ResolveDid)
mux.HandleFunc("/xrpc/com.atproto.identity.resolveHandle", idServer.ResolveHandle)
mux.HandleFunc("/xrpc/com.atproto.identity.resolveIdentity", idServer.ResolveIdentity)
```

- [ ] **Step 2: Verify full build**

```bash
go build ./cmd/pear/
```

Expected: compiles without errors.

- [ ] **Step 3: Run existing tests**

```bash
go test ./internal/identity/...
```

Expected: existing tests pass (the `WrappedDirectory` tests should still work since we didn't change its interface).

- [ ] **Step 4: Commit**

```bash
git add cmd/pear/main.go
git commit -m "pear: register com.atproto.identity resolve routes"
```

---

### Task 4: Run linter and full test suite

- [ ] **Step 1: Run golangci-lint**

```bash
golangci-lint run ./internal/identity/... ./cmd/pear/...
```

Expected: no new lint errors.

- [ ] **Step 2: Run full Go tests**

```bash
go test ./...
```

Expected: all tests pass.

- [ ] **Step 3: Final commit if lint fixes needed**

```bash
git add -A
git commit -m "fix: address lint issues in identity resolve handlers"
```

---

## Design Decisions

1. **No code generation** — `com.atproto.*` lexicons are not in `lexgen.json`. Types are defined inline in `server.go` since they're small (3 output structs). Adding `com.atproto.*` to lexgen would generate types for all existing `com.atproto.repo.*` lexicons too, which is unnecessary churn.

2. **`identity.Directory` over raw `hive.Hive`** — The new handlers use `s.directory` (a `WrappedDirectory` = hive + default directory) instead of `s.hive` directly. This means habitat-hosted identities resolve locally via hive first, and external DIDs/handles fall back to the public AT Protocol directory. The existing `ServeDIDDoc` and `ServeHandle` methods continue using `s.hive` directly since they only serve habitat identities.

3. **No authentication required** — These are public identity resolution endpoints, matching the AT Protocol convention. No `authn.NewValidator` calls.

4. **Error mapping** — `identity.ErrDIDNotFound` → `DidNotFound` (404), `identity.ErrHandleNotFound` → `HandleNotFound` (404), `identity.ErrDIDDeactivated` → `DidDeactivated` (404). Server errors use 500.
