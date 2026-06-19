# OAuth login: select org / hosting provider

## Problem

`frontend/src/routes/oauth-login.tsx` always sends the OAuth authorize request to `__HABITAT_DOMAIN__` (the managed Habitat hosting domain), because `AuthManager` bakes a single `serverDomain` into its OAuth `Configuration` at construction time. This is wrong for orgs hosted on a self-hosted/custom domain — there is no way to log in to those orgs from this page.

Separately, when a user is redirected to `/oauth-login` right after creating an org via `org/create.tsx`, the org's domain is known (it was just used to create the org) but isn't passed along, so the login page can't pre-select it.

## Goals

- Let the user pick which org/hosting provider to authenticate against on `oauth-login.tsx`, with the OAuth authorize/token requests going to that domain.
- Pre-fill that selection when arriving from `org/create.tsx` after creating an org.
- Reuse the existing "Managed hosting by Habitat" vs "Custom instance" pattern already built for `org/create.tsx`, rather than inventing a new UI.

- Let `docs` reuse the same login UI (selector + handle form) as `frontend`, since both are actively maintained Habitat apps, without duplicating the selector/lookup logic.

## Non-goals

- `calendar` and `greensky` apps' login pages do not get an org selector. They keep logging in against `__HABITAT_DOMAIN__` only, and are otherwise unaffected beyond the `AuthManager` signature change in section 1.
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

`calendar`, `greensky`:
- `main.tsx`: `new AuthManager(appName, __DOMAIN__, onUnauthenticated)` (drop the `__HABITAT_DOMAIN__` arg).
- `routes/login.tsx`: pass `domain={__HABITAT_DOMAIN__}` to `<AuthForm />`, unchanged otherwise (still plain `AuthForm`, no selector).

### 3. Shared components, moved into `typescript/internal`

Everything org-selector-related becomes shared so both `frontend` and `docs` can use it (`calendar`/`greensky` are untouched, per non-goals).

**`typescript/internal/src/components/instanceQueries.ts`** (new) — move `describeInstance` here from `frontend/src/queries/instance.ts` (same implementation, just relocated so it's available to any app via `internal`). Delete the old `frontend/src/queries/instance.ts` and update its one caller (`org/create.tsx`) to import from `internal`.

**`typescript/internal/src/components/HostingSelector.tsx`** (new) — extract the existing inline "Hosting" `ToggleGroup` (managed vs custom) + custom-domain `Input` + `describeInstance` name lookup from `org/create.tsx` (lines ~70-129, ~178-211):

```ts
interface HostingSelectorProps {
  defaultDomain: string; // the managed-hosting domain (app passes its __HABITAT_DOMAIN__)
  value: string; // resolved domain
  onChange: (domain: string) => void;
  disabled?: boolean; // e.g. when an invite token fixes the domain
}
```

Internally it owns the managed/custom toggle state and the `describeInstance` lookup effect (debounced via the existing `useEffect`/cancellation pattern), and calls `onChange` with the resolved domain (either `defaultDomain` or the typed custom domain) whenever it changes. Takes `defaultDomain` as a prop rather than reading `__HABITAT_DOMAIN__` directly, matching how `internal` already avoids referencing app-injected build globals (e.g. `AuthManager`/`clientMetadata` take `domain` as constructor args instead).

`org/create.tsx` is updated to use this component for its existing "Hosting" field (the invite-token case, where the domain is fixed and displayed read-only, is unchanged — `HostingSelector` is simply not rendered when `invite` is set, same as today).

**`typescript/internal/src/components/LoginForm.tsx`** (new) — composes `HostingSelector` + `AuthForm` into the full login page body:

```ts
interface LoginFormProps {
  authManager: AuthManager;
  redirectUrl: string;
  defaultDomain: string; // the managed-hosting domain (app passes its __HABITAT_DOMAIN__)
  serverError?: string;
  defaultHandle?: string;
  customDomain?: string; // pre-fill, e.g. from org/create.tsx redirect; if it differs from defaultDomain, the selector starts on "custom"
}
```

It holds the resolved-domain state (seeded from `customDomain ?? defaultDomain`, with the toggle starting on "custom" iff `customDomain` is set and differs from `defaultDomain`), renders `HostingSelector` above the handle field, and renders `AuthForm` with that resolved domain. Both `HostingSelector` and `LoginForm` are exported from `internal`'s `index.ts`.

### 4. `AuthForm` (`typescript/internal/src/AuthForm.tsx`)

Add a required `domain: string` prop. `loginUrl` is called as `authManager.loginUrl(handle, redirectUrl, domain)`. (Used directly by `calendar`/`greensky`'s plain login pages, and internally by `LoginForm`.)

### 5. `oauth-login.tsx` (`frontend`) and `login.tsx` (`docs`)

Both become thin wrappers around `LoginForm`:

- `loginSearchSchema` (frontend) gains `domain: z.string().optional()`. `docs`'s `login.tsx` doesn't currently take search params, so it just omits `defaultHandle`/`customDomain`.
- Each renders `<LoginForm authManager={authManager} redirectUrl={...} defaultDomain={__HABITAT_DOMAIN__} serverError={error} defaultHandle={handle} customDomain={domain} />`.

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
- Manual: visit `docs`'s `/login` directly, confirm it shows the same selector and logs in to either managed or a custom domain.
- Verify `calendar`/`greensky` login still works unchanged (no selector, always managed domain).
