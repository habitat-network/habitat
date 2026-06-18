# Instance settings, invite links, and org-creation enforcement (parts 2+3)

## Context

Part 1 (already implemented, in `internal/instanceadmin`) gave each `pear` instance a bootstrap admin account and cookie-based session login at `/admin`. This spec covers the next two pieces from the original self-hosting plan:

- **Part 2**: the admin page lets the instance admin configure the instance manager name and org-creation policy (open vs invite-only), and generate invite links when invite-only.
- **Part 3**: the org-creation flow (both the `pear` server and the `frontend` React app) honors that policy — invite links carry instance metadata, and any instance can be queried for its public metadata via a new `describeInstance` endpoint (this *is* the "`com.describeInstance`" the original ask deferred to "eventually" — turns out we need it now, for the case where a user manually enters a custom instance domain with no invite link).

Per user direction, all instance-admin operations (existing login/logout, and the new settings/invite operations) should be modeled as proper XRPC lexicons rather than ad-hoc form posts, consistent with the rest of the codebase (`network.habitat.org.*`, etc). **Reworking the already-shipped login/logout into lexicons is deferred to right after this work lands** — this spec's implementation only adds new lexicons (`network.habitat.admin.getSettings`, `updateSettings`, `issueInvite`, and the public `network.habitat.instance.describeInstance`); it does not touch `/admin/login` or `/admin/logout`.

## A. Data model (`internal/instanceadmin`)

Two new gorm models, `AutoMigrate`'d alongside the existing ones in `NewStore`:

- `instanceSettings` — singleton row (fixed `ID = 1`): `ManagerName string`, `OrgCreationPolicy string` (`"open"` | `"invite_only"`, default `"open"`), `SigningSecret string` (base64, 32 random bytes — generated once, same style as `internal/org/store.go:133-137`).
- `instanceInvite` — `Token string` (primary key; this is the JWT's `jti`, a random hex string, not the JWT itself), `CreatedAt time.Time`, `ExpiresAt time.Time` (7 days from creation), `UsedAt *time.Time` (nil until redeemed).

`instanceSettings` is created lazily on first access (analogous to `instanceAdminAccount`'s bootstrap) with `ManagerName: ""`, `OrgCreationPolicy: "open"`, and a freshly generated `SigningSecret`, the first time `GetSettings`/`IssueInvite` is called — no need to thread this through the existing `Bootstrap` call.

`Store` gains:
```go
GetSettings(ctx context.Context) (managerName, orgCreationPolicy string, err error)
UpdateSettings(ctx context.Context, managerName, orgCreationPolicy string) error
IssueInvite(ctx context.Context) (token string, err error) // returns a signed JWT
RedeemInvite(ctx context.Context, token string) error       // validates + marks used; one generic error for all failure modes
GetOrgCreationPolicy(ctx context.Context) (string, error)   // convenience read used by internal/org
```

`IssueInvite` creates an `instanceInvite` row (random `jti`), then signs a JWT (jose HS256, same library as `internal/login/password.go`) with claims `jti`, `domain` (this instance's pear domain, passed into `NewStore`/`NewServer`), `name` (current `ManagerName`), `exp`. `RedeemInvite` parses+verifies the JWT against `SigningSecret`, loads the `instanceInvite` row by `jti`, and fails if missing, expired, or already used; on success sets `UsedAt`.

`UpdateSettings` validates `orgCreationPolicy` is one of `"open"`/`"invite_only"` (`errors.New`-defined `ErrInvalidPolicy`, returned as 400).

## B. New lexicons + handlers (`internal/instanceadmin`)

All under `network.habitat.admin.*`, JSON XRPC over `/xrpc/...`, gated by session cookie (existing `instanceAdminSession`):

- `network.habitat.admin.getSettings` — query, no input. Output: `{ manager_name: string, org_creation_policy: string }`.
- `network.habitat.admin.updateSettings` — procedure. Input: `{ manager_name: string, org_creation_policy: string }`. Output: same shape as `getSettings` (the updated values), so the page can refresh its display from the response.
- `network.habitat.admin.issueInvite` — procedure, no input. Output: `{ token: string }` (the signed JWT; the page builds the full shareable URL client-side as `https://<frontend-domain>/org/create?token=<token>`, using a frontend domain value the server injects into the rendered HTML template — same `fFrontendDomain` flag already plumbed into `main.go` for `login.NewPasswordProvider`).

Because these are called via `fetch()` from the admin page's JS rather than full-page navigation, a failed check here should *not* redirect like the existing page route does. Rather than wrapping handlers in middleware, follow the existing `authn.Validate(w, r, s.auth)` pattern used elsewhere in the codebase (e.g. `internal/org/server.go`) — a helper called at the top of each handler that returns `(ok bool)` and writes the failure response itself:
- `requireSessionPage(w, r) bool` — on failure, redirects to `/admin/login` and returns `false`; called at the top of `ServeAdminHome` (replaces today's `RequireSession`-wrapped route registration in `main.go`, which becomes a plain `mux.HandleFunc("/admin", instanceAdminServer.ServeAdminHome)`).
- `requireSessionAPI(w, r) bool` — on failure, writes a 401 via `utils.WriteHTTPError` and returns `false`; called at the top of the three lexicon handlers above.

Both share the same cookie-read + `ValidateSession` logic internally; only the failure response differs.

The `/admin` page (`home.html`) is extended with a settings form (manager name input, open/invite-only radio) and, when invite-only, a "Generate invite link" button. A small inline `<script>` block does `fetch` calls to the three endpoints above (cookies are same-origin, sent automatically) and updates the DOM — no separate JS bundle/build step, consistent with this page being plain server-rendered HTML.

## C. `network.habitat.instance.describeInstance`

New lexicon, new namespace (separate from both `org.*` and `admin.*` since it's public, instance-level, unauthenticated), modeled on `com.atproto.server.describeServer`. Handler lives in `instanceadmin.Server` since it reads the same settings.

- `GET /xrpc/network.habitat.instance.describeInstance` — query, no input, **no auth**.
- Output: `{ name: string, invite_required: bool }` (`invite_required` is `org_creation_policy == "invite_only"`).

## D. Org-creation enforcement (`internal/org`)

- `lexicons/network/habitat/org/create.json` gains an optional `invite_token` string property; regenerate `api/habitat/org_create.go` via `moon :generate`.
- `org.Server` (`internal/org/server.go`) takes a new constructor dependency — a small consumer-defined interface so `internal/org` doesn't import `internal/instanceadmin`'s concrete store:
  ```go
  type InstancePolicy interface {
      GetOrgCreationPolicy(ctx context.Context) (string, error)
      RedeemInvite(ctx context.Context, token string) error
  }
  ```
- `CreateOrg`: after existing field validation, if `GetOrgCreationPolicy` returns `"invite_only"`: require `req.InviteToken != ""`, then call `RedeemInvite`; any error (empty token, invalid/expired/reused) results in `403` with a single generic message: `"an invite link is required to create an org on this instance"` (don't distinguish reasons to avoid leaking token-validity details).
- Wiring in `cmd/pear/main.go`: pass `instanceAdminStore` (already constructed in part 1) as the `InstancePolicy` argument to `org.NewServer`.

## E. Frontend `org/create` page (`frontend/src/routes/org/create.tsx`)

- Read `token` from the route's search params (TanStack Router `validateSearch`).
- If `token` present: base64url-decode the JWT payload (display only — no signature check client-side; the server re-validates on submit) to read `domain` and `name`. Render a disabled `Input` showing `name` in place of any instance picker. All `procedure()`/`query()` calls for this page use `domain` from the token instead of `__HABITAT_DOMAIN__`.
- If no `token`: render a small picker — "Managed hosting by Habitat" (radio/toggle, default selected, uses `__HABITAT_DOMAIN__`) vs. "Custom instance" (reveals a text `Input` for a domain). On blur/change of the custom domain, call `network.habitat.instance.describeInstance` against that domain (reusing the existing `{ unauthenticated: true, domain }` option already used by `query`/`procedure`) and show the returned `name` read-only beneath it. No client-side blocking on `invite_required` — purely informational.
- `onSubmit`: include `invite_token: token` in the `network.habitat.org.create` body when a token is present (omit otherwise). If the server 403s (invite-only, no/invalid token), the existing `errors.root` banner already surfaces `err.message` — no new error-handling path needed.

## Testing

- `internal/instanceadmin`: table-driven tests for `GetSettings`/`UpdateSettings` (including the invalid-policy rejection), `IssueInvite`/`RedeemInvite` (valid, expired, already-used, tampered-signature, wrong-instance-secret), and `httptest`-based handler tests for the three new XRPC endpoints plus the `RequireSessionPage`/`RequireSessionAPI` split. `describeInstance` handler test (with/without invite-only).
- `internal/org`: extend `CreateOrg` tests with a fake `InstancePolicy` — open policy ignores token, invite-only rejects missing/invalid token and accepts a valid one (verifying `RedeemInvite` was called).
- Frontend: skip automated tests (no existing test setup for this route found); verify manually per the plan's verification section.

## Verification

1. `go test ./internal/instanceadmin/... ./internal/org/...`
2. `moon :generate` (regenerate `org_create.go` from the updated lexicon) then `go build ./...` in both modules.
3. Run `pear`, log into `/admin`, set policy to invite-only with a manager name, generate an invite link, confirm it renders once.
4. `moon frontend:dev`; visit `/org/create?token=<generated token>` → confirm the read-only instance name shows and org creation succeeds end-to-end.
5. Visit `/org/create` with no token, pick "Custom instance", enter the same instance's domain → confirm `describeInstance` populates the name, and submitting without a token gets the 403 message surfaced in the form.
6. Set policy back to open → confirm `/org/create` with no token succeeds without any invite_token.
