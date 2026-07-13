# Frontend atproto oauth-client Migration — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the frontend `AuthManager`'s hand-rolled `openid-client` OAuth flow with `@atproto/oauth-client-browser`, pointed at Habitat's now-atproto-compliant OAuth server via the pear domain as a service URL.

**Architecture:** `AuthManager` keeps its public method names but holds a `BrowserOAuthClient` internally. Sign-in passes the pear domain (a service/entryway URL) to the client, which resolves Habitat's protected-resource → authorization-server metadata and runs PAR/PKCE/DPoP against Habitat. The user enters their handle on Habitat's own disambiguation page. The library owns token storage, refresh, DPoP, and cross-tab coordination, so the zustand store, JWT decoding, manual refresh, and Web Locks logic are all deleted.

**Tech Stack:** TypeScript, React 19, TanStack Router, `@atproto/oauth-client-browser` (already a dependency at `^0.3.34`), Vite, Moon.

## Global Constraints

- Package with the `AuthManager` / `AuthForm` / `habitatClient` / `clientMetadata` code: `typescript/internal/`.
- Frontend app consuming them: `frontend/`.
- `typescript/internal` has **no test runner**; `AuthManager` is a browser-only wrapper (IndexedDB, redirects, DPoP). Verification for every task is **typecheck + build** via `moon frontend:build` (runs `tsc --build --force && vite build`, building `typescript/internal` first) plus lint. There is a dedicated final manual end-to-end task.
- Lint: oxlint (`.oxlintrc.json`); format: Prettier. Run `moon frontend:lint-check` / `moon typescript/internal:lint-check` as available, or `moon :lint-check`.
- Never edit generated files (`typescript/api/`).
- `clientMetadata(appName, domain)` already produces the atproto client-metadata document and it is served at `https://${domain}/client-metadata.json` by `habitatAppVitePlugin`. Do not change how it is served.
- The pear/server domain is passed to `AuthManager` as `serverDomain` (3rd constructor arg, wired from `__HABITAT_DOMAIN__` in `frontend/src/main.tsx`).
- Consumers only ever read `.did` off `getAuthInfo()`. Preserve that return shape (`{ did } | undefined`) to avoid touching ~10 call sites.

---

### Task 1: Rewrite `AuthManager` internals over `BrowserOAuthClient`

**Files:**
- Modify: `typescript/internal/src/authManager.ts` (full rewrite of the class body)
- Reference (do not modify): `typescript/internal/src/clientMetadata.ts`, `typescript/internal/node_modules/@atproto/oauth-client-browser/dist/browser-oauth-client.d.ts`, `node_modules/.pnpm/@atproto+oauth-client@*/node_modules/@atproto/oauth-client/dist/oauth-session.d.ts`

**Interfaces:**
- Consumes: `clientMetadata(appName: string, domain: string)` → the client-metadata document object; `BrowserOAuthClient` from `@atproto/oauth-client-browser`; `OAuthSession` (`.sub: string`, `.fetchHandler(pathname, init?) => Promise<Response>`, `.signOut() => Promise<void>`).
- Produces (public API other tasks rely on):
  - `constructor(appName: string, domain: string, serverDomain: string, onUnauthenticated: () => void)`
  - `init(): Promise<void>` — idempotent; processes callback or restores session.
  - `getAuthInfo(): { did: string } | undefined`
  - `login(): void` — starts sign-in redirect against the pear domain.
  - `fetch(path: string, method?: string, body?: BodyInit | null, headers?: Headers): Promise<Response>`
  - `logout: () => void` (arrow, preserves `this` when passed as `onLogout`)
  - `class UnauthenticatedError extends Error`

- [ ] **Step 1: Replace the file contents**

Full new contents of `typescript/internal/src/authManager.ts`:

```typescript
import clientMetadata from "./clientMetadata";
import {
  BrowserOAuthClient,
  type OAuthSession,
} from "@atproto/oauth-client-browser";

export class AuthManager {
  private serverDomain: string;
  private client: BrowserOAuthClient;
  private session: OAuthSession | undefined;
  private onUnauthenticated: () => void;
  // Memoized so init() is safe to call from every router beforeLoad.
  private initPromise: Promise<void> | undefined;

  constructor(
    appName: string,
    domain: string,
    serverDomain: string,
    onUnauthenticated: () => void,
  ) {
    this.serverDomain = serverDomain;
    this.onUnauthenticated = onUnauthenticated;
    this.client = new BrowserOAuthClient({
      clientMetadata: clientMetadata(appName, domain),
      // Habitat resolves handles server-side; the client only needs to reach
      // the pear domain's OAuth metadata.
      handleResolver: `https://${serverDomain}`,
    });
  }

  // Processes an OAuth callback if present in the URL, otherwise restores an
  // existing session. Idempotent: the underlying client.init() must run once.
  init(): Promise<void> {
    if (!this.initPromise) {
      this.initPromise = this.client.init().then((result) => {
        this.session = result?.session;
      });
    }
    return this.initPromise;
  }

  getAuthInfo() {
    if (!this.session) {
      return undefined;
    }
    return { did: this.session.sub };
  }

  // Begins the OAuth flow against the pear domain as a service URL. The atproto
  // client resolves the pear domain's oauth-protected-resource -> authorization
  // server and runs PAR/PKCE/DPoP against Habitat. No login_hint is sent; the
  // user picks their handle on Habitat's disambiguation page.
  login() {
    void this.client.signInRedirect(`https://${this.serverDomain}`);
  }

  logout = () => {
    void this.session?.signOut();
    this.session = undefined;
    this.onUnauthenticated();
  };

  async fetch(
    path: string,
    method: string = "GET",
    body?: BodyInit | null,
    headers?: Headers,
  ) {
    if (!this.session) {
      return this.handleUnauthenticated();
    }
    if (!headers) {
      headers = new Headers();
    }
    headers.append("Habitat-Auth-Method", "oauth");
    const response = await this.session.fetchHandler(
      new URL(path, `https://${this.serverDomain}`).toString(),
      { method, body, headers },
    );
    if (response.status === 401) {
      return this.handleUnauthenticated();
    }
    return response;
  }

  private handleUnauthenticated(): Response {
    this.logout();
    throw new UnauthenticatedError();
  }
}

export class UnauthenticatedError extends Error {}
```

- [ ] **Step 2: Verify the `handleResolver` option and `signInRedirect` exist**

Run:
```bash
grep -n "handleResolver\|signInRedirect\|clientMetadata" typescript/internal/node_modules/@atproto/oauth-client-browser/dist/browser-oauth-client-options.d.ts typescript/internal/node_modules/@atproto/oauth-client-browser/dist/browser-oauth-client.d.ts
```
Expected: `signInRedirect` is a method on `BrowserOAuthClient`; `BrowserOAuthClientOptions` accepts `clientMetadata` and a handle-resolver option. If the handle-resolver option has a different name (e.g. `handleResolver` vs nested under identity options) or the constructor requires additional identity-resolution config, adjust the constructor accordingly — the client only needs to reach the pear domain's metadata, so a minimal valid config is the target.

- [ ] **Step 3: Typecheck the internal package's exported surface**

Run:
```bash
moon typescript/internal:build
```
Expected: PASS (Vite build of the package succeeds; no unresolved imports). If `@atproto/oauth-client-browser` types complain about the `clientMetadata` shape, confirm `clientMetadata.ts`'s `satisfies` clause still matches and reconcile.

- [ ] **Step 4: Commit**

```bash
git add typescript/internal/src/authManager.ts
git commit -m "[FE] Rewrite AuthManager over atproto oauth-client-browser

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Drop `openid-client` `DPoPOptions` from `habitatClient`

**Files:**
- Modify: `typescript/internal/src/habitatClient.ts`

**Interfaces:**
- Consumes: `AuthManager.fetch(path, method?, body?, headers?)` from Task 1 (note: the trailing `options?: DPoPOptions` argument no longer exists).
- Produces: `AuthedOptions` no longer has a `fetchOptions` field; `query`/`procedure`/`authedRequest` call `authManager.fetch` with four args max.

- [ ] **Step 1: Remove the `openid-client` import**

In `typescript/internal/src/habitatClient.ts`, delete line 61:
```typescript
import { DPoPOptions } from "openid-client";
```

- [ ] **Step 2: Remove `fetchOptions` from `AuthedOptions`**

Change the `AuthedOptions` interface (around line 333) from:
```typescript
interface AuthedOptions {
  unauthenticated?: false;
  authManager: AuthManager;
  headers?: Headers;
  fetchOptions?: DPoPOptions;
}
```
to:
```typescript
interface AuthedOptions {
  unauthenticated?: false;
  authManager: AuthManager;
  headers?: Headers;
}
```

- [ ] **Step 3: Drop the `fetchOptions` argument at the two call sites**

In `query` (around line 388), change:
```typescript
    : await (options as AuthedOptions).authManager.fetch(
        path,
        "GET",
        null,
        options.headers,
        (options as AuthedOptions).fetchOptions,
      );
```
to:
```typescript
    : await (options as AuthedOptions).authManager.fetch(
        path,
        "GET",
        null,
        options.headers,
      );
```

In `authedRequest` (around line 422), change:
```typescript
const authedRequest = (
  endpoint: string,
  params: unknown,
  options: AuthedOptions,
) =>
  options.authManager.fetch(
    "/xrpc/" + endpoint,
    "POST",
    JSON.stringify(params),
    options.headers,
    options.fetchOptions,
  );
```
to:
```typescript
const authedRequest = (
  endpoint: string,
  params: unknown,
  options: AuthedOptions,
) =>
  options.authManager.fetch(
    "/xrpc/" + endpoint,
    "POST",
    JSON.stringify(params),
    options.headers,
  );
```

- [ ] **Step 4: Confirm no remaining `fetchOptions` / `DPoPOptions` references**

Run:
```bash
grep -rn "fetchOptions\|DPoPOptions" typescript/internal/src frontend/src
```
Expected: no matches.

- [ ] **Step 5: Build to typecheck**

Run:
```bash
moon typescript/internal:build
```
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add typescript/internal/src/habitatClient.ts
git commit -m "[FE] Drop openid-client DPoPOptions from habitatClient

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Reduce `AuthForm` to a single sign-in button

**Files:**
- Modify: `typescript/internal/src/AuthForm.tsx`

**Interfaces:**
- Consumes: `AuthManager.login()` from Task 1.
- Produces: `AuthForm` props become `{ authManager, serverError?, orgLoginUrl? }` — the `redirectUrl` and `defaultHandle` props are removed.

- [ ] **Step 1: Replace the component with a button-only form**

Full new contents of `typescript/internal/src/AuthForm.tsx`:

```tsx
import type { AuthManager } from "./authManager";
import { useId } from "react";
import { Button } from "./components/ui";

interface AuthFormProps {
  authManager: AuthManager;
  serverError?: string;
  orgLoginUrl?: string;
}

export default function AuthForm({
  authManager,
  serverError,
  orgLoginUrl,
}: AuthFormProps) {
  const errorId = useId();

  return (
    <div className="min-h-screen flex items-center justify-center p-4">
      <div className="w-full max-w-sm rounded-xl border bg-background p-8 shadow-sm">
        <Button
          className="w-full"
          onClick={() => authManager.login()}
          aria-describedby={errorId}
        >
          Sign In with Habitat
        </Button>
        {serverError && (
          <small id={errorId} className="mt-4 block text-destructive">
            {serverError}
          </small>
        )}
        {orgLoginUrl && (
          <Button
            variant="link"
            className="mt-6"
            size="sm"
            render={<a href={orgLoginUrl} />}
          >
            Add this app to your organization
          </Button>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Build to typecheck**

Run:
```bash
moon typescript/internal:build
```
Expected: PASS. (`useForm`, `useMutation`, `Input`, `Field*` imports are gone; if the build complains about an unused import, remove it.)

- [ ] **Step 3: Commit**

```bash
git add typescript/internal/src/AuthForm.tsx
git commit -m "[FE] AuthForm: single Sign In with Habitat button

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Update frontend consumers (`oauth-login`, `__root`)

**Files:**
- Modify: `frontend/src/routes/oauth-login.tsx`
- Modify: `frontend/src/routes/__root.tsx:17`

**Interfaces:**
- Consumes: `AuthForm` props `{ authManager, serverError?, orgLoginUrl? }` from Task 3; `AuthManager.init()` from Task 1.

- [ ] **Step 1: Simplify `oauth-login.tsx`**

Full new contents of `frontend/src/routes/oauth-login.tsx`:

```tsx
import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";

import { AuthForm } from "internal";

export const Route = createFileRoute("/oauth-login")({
  validateSearch: z.object({
    error: z.string().optional(),
  }),
  component() {
    const { error } = Route.useSearch();
    const { authManager } = Route.useRouteContext();
    return <AuthForm authManager={authManager} serverError={error} />;
  },
});
```

- [ ] **Step 2: Swap `maybeExchangeCode` for `init` in `__root.tsx`**

In `frontend/src/routes/__root.tsx`, change line 17 from:
```typescript
    await context.authManager.maybeExchangeCode();
```
to:
```typescript
    await context.authManager.init();
```

- [ ] **Step 3: Confirm no remaining `maybeExchangeCode` / `loginUrl` / `defaultHandle` references**

Run:
```bash
grep -rn "maybeExchangeCode\|loginUrl\|defaultHandle" frontend/src typescript/internal/src
```
Expected: no matches.

- [ ] **Step 4: Full frontend build (typecheck + Vite)**

Run:
```bash
moon frontend:build
```
Expected: PASS (`tsc --build --force` typechecks frontend + the imported `internal` sources, then `vite build` succeeds).

- [ ] **Step 5: Commit**

```bash
git add frontend/src/routes/oauth-login.tsx frontend/src/routes/__root.tsx
git commit -m "[FE] Wire routes to AuthManager.init and handle-less login

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Remove the `openid-client` dependency

**Files:**
- Modify: `typescript/internal/package.json:50`

**Interfaces:** none (dependency cleanup).

- [ ] **Step 1: Confirm nothing imports `openid-client`**

Run:
```bash
grep -rn "openid-client" typescript/internal/src frontend/src
```
Expected: no matches. (If any remain, they belong to a prior task — stop and resolve there.)

- [ ] **Step 2: Remove the dependency line**

In `typescript/internal/package.json`, delete line 50:
```json
    "openid-client": "^6.8.1",
```

- [ ] **Step 3: Reinstall to update the lockfile**

Run:
```bash
pnpm install
```
Expected: completes; `openid-client` removed from `typescript/internal`'s resolved deps.

- [ ] **Step 4: Rebuild to confirm nothing broke**

Run:
```bash
moon frontend:build
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add typescript/internal/package.json pnpm-lock.yaml
git commit -m "[FE] Remove openid-client dependency

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: End-to-end verification of the login flow

This task has no code; it validates the two spec risks (scope acceptance, hash-routing callback) against a running stack. There is no automated harness for the browser OAuth flow, so this is a manual/driven check.

**Files:** none.

- [ ] **Step 1: Start the dev stack**

Run (in a background-capable shell):
```bash
moon frontend:dev
```
This starts the frontend + pear backend + Caddy + ngrok. Wait for the frontend to be reachable at its `local.habitat.network` domain.

- [ ] **Step 2: Drive the login flow**

Use the `/run` or `/verify` skill (or a browser MCP) to:
1. Navigate to the frontend, which should redirect to `/oauth-login`.
2. Click "Sign In with Habitat".
3. Confirm the browser is redirected to Habitat and lands on the disambiguation page (`/ui/login/disambiguate`) — i.e. the PAR request against the pear domain succeeded and no `login_hint` was required.
4. Enter a test handle, complete the PDS login, and confirm redirect back to the frontend with an established session (authenticated content renders; `getAuthInfo()?.did` is populated).

- [ ] **Step 3: Check the scope risk**

While driving Step 2, watch the network requests / server logs for the `/oauth/par` and `/oauth/token` calls. Confirm the request carries `scope=atproto transition:generic` (from `client-metadata.json`) and the server does **not** respond with `invalid_scope`. If it does, reconcile `client-metadata.json`'s `scope` with the server's advertised `scopes_supported` before considering the migration complete, and note the resolution.

- [ ] **Step 4: Check the hash-routing callback**

Confirm that after the redirect back, the URL's `code`/`state`/`iss` params are consumed (not left dangling) and a reload keeps the user logged in (session restored from IndexedDB by `client.init()`).

- [ ] **Step 5: Record the result**

Note the outcome (pass, or the specific failure + fix) in the PR description / commit trailer. No commit required if all checks pass.

---

## Notes for the implementer

- The atproto client stores sessions in IndexedDB keyed by DID and manages refresh + cross-tab coordination itself — do not reintroduce any manual token/refresh/locking logic.
- If Task 1 Step 2 reveals the browser client needs a different identity-resolution config to construct successfully, keep the config minimal: the only requirement is that `signInRedirect(pearDomainUrl)` can resolve the pear domain's OAuth metadata. Habitat resolves the user's handle server-side.
- Do not touch `typescript/apps/*` apps that use `openid-client` independently; this migration is scoped to `frontend/` + `typescript/internal/`.
