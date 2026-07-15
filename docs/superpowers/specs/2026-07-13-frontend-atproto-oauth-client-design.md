# Frontend `AuthManager`: migrate from `openid-client` to `@atproto/oauth-client-browser`

## Motivation

The frontend `AuthManager` (`typescript/internal/src/authManager.ts`) is an OAuth
**client of Habitat's own OAuth server** (`serverDomain` / the pear domain), not of
the user's PDS. Habitat brokers to the PDS behind the scenes.

Today `AuthManager` hand-drives the flow with `openid-client`: it constructs a
minimal `Configuration` pointing at `/oauth/authorize` and `/oauth/token`, builds
authorization URLs, exchanges the code, decodes the JWT to track `expiresAt`,
manually refreshes tokens, and serializes cross-tab refresh with the Web Locks API.

Now that Habitat's OAuth server is atproto-spec compliant — it advertises
`.well-known/oauth-authorization-server` (PAR required, DPoP, `client_id_metadata_document_supported`),
`.well-known/oauth-protected-resource` pointing at itself, and the `iss` response
parameter — the frontend can use the standard atproto client instead of a bespoke
`openid-client` integration. `@atproto/oauth-client-browser` is already a dependency.

Switching lets the library own PAR, PKCE, DPoP, token storage, refresh, and
cross-tab coordination, and deletes a meaningful amount of hand-rolled auth code.

## Approach

Point the atproto client at **Habitat as an entryway/service URL** rather than at a
handle.

`@atproto/oauth-client`'s `signIn(input)` (via `OAuthResolver.resolve`, see
`packages/oauth/oauth-client/src/oauth-resolver.ts`) resolves `input` two ways:

- **A handle/DID** → resolves the user's *real PDS* authorization server and sets
  `login_hint=handle`. This would bypass Habitat's broker entirely — wrong for our
  model.
- **A service / entryway URL** → fetches that URL's `oauth-protected-resource` →
  authorization server. Pointing this at `https://${serverDomain}` (the pear domain)
  makes the client run PAR / PKCE / DPoP **against Habitat**. Correct. In this path
  the library sends **no `login_hint`**.

The server already handles the no-handle case: `HandleAuthorize` redirects to
Habitat's own disambiguation page (`/ui/login/disambiguate`) where the user enters
their handle. So the frontend collects no handle; it just kicks off the flow against
the pear domain.

The library owns everything the old code did by hand: token storage (IndexedDB),
refresh, cross-tab coordination, DPoP proofs. We therefore **delete** the
zustand/localStorage store, `decodeJwt`, the manual `expiresAt` refresh logic, and
the Web Locks serialization.

## Components

### `AuthManager` — same public shape, new internals

Keep the class and its method names so the existing call sites don't churn. Consumers
only ever read `.did` off `getAuthInfo()`, so preserving that shape keeps them
untouched.

- **constructor** — builds a `BrowserOAuthClient` from `clientMetadata(appName, domain)`
  (already produced and served at `/client-metadata.json` by
  `habitatAppVitePlugin`). Stores `serverDomain` and `onUnauthenticated`.
- **`init()`** (replaces `maybeExchangeCode`) — `await client.init()` once, guarded by
  a memoized promise so it is idempotent across `beforeLoad` calls. `init()` processes
  the OAuth callback if callback params are present in the URL, otherwise restores an
  existing session. Stores the resulting `OAuthSession | undefined`.
- **`getAuthInfo()`** — returns `{ did: session.sub } | undefined`. This is the only
  field consumers read, so every call site keeps working unchanged.
- **`login()`** (replaces `loginUrl`) — `client.signInRedirect("https://" + serverDomain)`.
  No handle argument.
- **`fetch(path, method, body, headers)`** — delegates to `session.fetchHandler`,
  still appends the `Habitat-Auth-Method: oauth` header, still maps a `401` response to
  `handleUnauthenticated()`. The `openid-client` `DPoPOptions` parameter is dropped
  (DPoP is now internal to the session).
- **`logout()`** — `session.signOut()` (revokes tokens) then `onUnauthenticated()`.

`UnauthenticatedError` is retained.

### Consumer touch-ups

- **`AuthForm.tsx`** — drop the handle `Input` and the react-hook-form plumbing;
  becomes a single "Sign in with Habitat" button calling `authManager.login()`.
  Removes the `defaultHandle` prop.
- **`oauth-login.tsx`** — drops the `handle` search param and `defaultHandle` prop.
- **`__root.tsx`** `beforeLoad` — `await context.authManager.init()` instead of
  `maybeExchangeCode()`.
- **`habitatClient.ts`** — remove the `DPoPOptions` import from `openid-client` and the
  `fetchOptions` field on `AuthedOptions` (no caller supplies it), and the argument
  threaded into `authManager.fetch`.
- **`typescript/internal/package.json`** — remove `openid-client` once nothing imports
  it.

## Data flow

1. User clicks "Sign in with Habitat" → `authManager.login()` →
   `client.signInRedirect("https://" + serverDomain)`.
2. Library resolves the pear domain's `oauth-protected-resource` → Habitat auth
   server, runs a PAR request (DPoP, PKCE), and redirects the browser to Habitat's
   `/oauth/authorize`.
3. Habitat sees no `login_hint` → redirects to `/ui/login/disambiguate`; user enters
   their handle; Habitat brokers the PDS OAuth flow.
4. Habitat redirects back to a registered `redirect_uri` with `code`, `state`, `iss`.
5. On app load, `authManager.init()` → `client.init()` processes the callback,
   exchanges the code for a DPoP-bound Habitat token, stores the session, and cleans
   the URL.
6. Resource requests go through `authManager.fetch` → `session.fetchHandler`, which
   attaches the access token and a fresh DPoP proof and auto-refreshes as needed.

## Risks / validation points

1. **Scope.** `client-metadata.json` requests `atproto transition:generic`; the server
   advertises `scopes_supported: ["atproto"]`; the atproto library requires an
   `atproto` scope. An older note recorded that pre-compliance pear clients had to send
   an empty scope or get `invalid_scope`. Verify the compliant server now accepts the
   `atproto` scope end-to-end. If the server still rejects it, reconcile the metadata
   before shipping.
2. **Hash routing.** The app uses `createHashHistory` when `__HASH_ROUTING__`. OAuth
   callback params arrive as `?code&state&iss` before the `#`. `client.init()` reads
   `window.location.search`, so it should work — verify the callback lands, the session
   is established, and the URL is cleaned.
3. **`openid-client` removal.** Only remove the dependency after confirming no remaining
   imports (`authManager.ts` and `habitatClient.ts` are the known ones).

## Out of scope

- Server-side OAuth changes (PAR/DPoP/`iss`) — already implemented on this branch.
- Other apps under `typescript/apps/*` that use `openid-client` independently.
