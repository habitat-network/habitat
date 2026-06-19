# OAuth login: select org / hosting provider

## Problem

`frontend/src/routes/oauth-login.tsx` always sends the OAuth authorize request to `__HABITAT_DOMAIN__` (the managed Habitat hosting domain), because `AuthManager` bakes a single `serverDomain` into its OAuth `Configuration` at construction time. This is wrong for orgs hosted on a self-hosted/custom domain — there is no way to log in to those orgs from this page.

Separately, when a user is redirected to `/oauth-login` right after creating an org via `org/create.tsx`, the org's domain is known (it was just used to create the org) but isn't passed along, so the login page can't pre-select it.

## Goals

- Let the user pick which org/hosting provider to authenticate against on `oauth-login.tsx`, with the OAuth authorize/token requests going to that domain.
- Pre-fill that selection when arriving from `org/create.tsx` after creating an org.
- Reuse the existing "Managed hosting by Habitat" vs "Custom instance" pattern already built for `org/create.tsx`, rather than inventing a new UI.

## Non-goals

- `calendar`, `greensky`, and `docs` apps' login pages do not get an org selector. They keep logging in against `__HABITAT_DOMAIN__` only.
- No change to how orgs are created, joined, or to the org-create/join backend APIs.

## Design

### 1. `AuthManager` (`typescript/internal/src/authManager.ts`)

Today the constructor takes `(appName, domain, serverDomain, onUnauthenticated)` and builds a single `client.Configuration` pointed at `serverDomain` once, at construction. This is fixed for the lifetime of the AuthManager instance, so it can't vary per login.

Change:
- Drop the `serverDomain` constructor param. Constructor becomes `(appName, domain, onUnauthenticated)`.
- Add a private `buildConfig(serverDomain: string): client.Configuration` that builds the `Configuration` from `clientMetadata(appName, domain)` plus `serverDomain`'s issuer/authorization/token endpoints. (`domain` — the app's own domain used for `client_id`/`redirect_uris` — stays fixed per app instance, as today.)
- `loginUrl(handle, redirectUri, serverDomain)` gains a third parameter. It calls `buildConfig(serverDomain)`, writes `serverDomain` to localStorage under a new key (next to the existing `state` key), and builds the authorization URL from that config.
- `maybeExchangeCode()` reads the stashed `serverDomain` from localStorage (in addition to `state`), calls `buildConfig(serverDomain)` to exchange the code, then writes `serverDomain` into the existing zustand-persisted store alongside `authInfo` so it survives reloads. The localStorage scratch key is cleared after a successful exchange (same lifecycle as `state` today).
- `fetch()` and the refresh-token-grant path read `serverDomain` from the persisted store (instead of the old fixed `this.serverDomain` field) to build the config / base URL for requests.
- If there is no persisted `serverDomain` (e.g. corrupted/cleared storage) `fetch()` should treat this the same as unauthenticated (`handleUnauthenticated()`), since requests can't be routed.

### 2. Call site updates for non-selector apps

`calendar`, `greensky`, `docs`:
- `main.tsx`: `new AuthManager(appName, __DOMAIN__, onUnauthenticated)` (drop the `__HABITAT_DOMAIN__` arg).
- `routes/login.tsx`: pass `domain={__HABITAT_DOMAIN__}` to `<AuthForm />`.

### 3. Shared hosting-selector component

Extract the existing inline "Hosting" `ToggleGroup` (managed vs custom) + custom-domain `Input` + `describeInstance` name lookup from `org/create.tsx` (lines ~70-129, ~178-211) into `frontend/src/components/HostingSelector.tsx`:

```ts
interface HostingSelectorProps {
  value: string; // resolved domain
  onChange: (domain: string) => void;
  disabled?: boolean; // e.g. when an invite token fixes the domain
}
```

Internally it owns the managed/custom toggle state and the `describeInstance` lookup effect (debounced via the existing `useEffect`/cancellation pattern), and calls `onChange` with the resolved domain (either `__HABITAT_DOMAIN__` or the typed custom domain) whenever it changes.

`org/create.tsx` is updated to use this component for its existing "Hosting" field (the invite-token case, where the domain is fixed and displayed read-only, is unchanged — `HostingSelector` is simply not rendered when `invite` is set, same as today).

### 4. `AuthForm` (`typescript/internal/src/AuthForm.tsx`)

Add a required `domain: string` prop. `loginUrl` is called as `authManager.loginUrl(handle, redirectUrl, domain)`.

### 5. `oauth-login.tsx`

- `loginSearchSchema` gains `domain: z.string().optional()`.
- Local state holds the resolved domain, initialized from the `domain` search param if present, else `__HABITAT_DOMAIN__`.
- Renders `HostingSelector` above the handle field, wired to that state.
- Passes the resolved domain to `<AuthForm domain={domain} .../>`.

### 6. `org/create.tsx`

After a successful org creation:
```ts
await navigate({
  to: "/oauth-login",
  search: { handle: admin_handle, domain: targetDomain },
});
```

## Testing

- Manual: create a managed org, confirm redirect to `/oauth-login` pre-selects managed hosting and logs in successfully.
- Manual: create an org with a custom instance domain (or via invite token), confirm redirect pre-selects that custom domain and OAuth authorize/token requests go to it.
- Manual: visit `/oauth-login` directly with no search params, confirm managed hosting is the default and login still works.
- Manual: switch the selector from managed to custom and back before submitting, confirm the authorize request goes to whichever domain is selected at submit time.
- Verify `calendar`/`greensky`/`docs` login still works unchanged (no selector, always managed domain).
