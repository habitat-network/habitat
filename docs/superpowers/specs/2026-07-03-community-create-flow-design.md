# Community Creation Flow

## Motivation

`website/src/pages/community-platform.astro` currently has no signup CTA — only a
Calendly link and a waitlist email capture. We want a "Get started now" button
that sends visitors into a dedicated community-creation flow hosted in
`frontend/`, analogous to the existing `frontend/src/routes/org/create.tsx` but
with copy/UX tailored to communities (simpler field set, two visual steps
instead of one dense form) and a subdomain auto-derived from the community
name instead of typed by hand.

A "community" is not a new backend concept — it's the existing `org` entity
under different marketing language. This flow calls the existing
`network.habitat.org.create` procedure with no lexicon or backend changes.

## Website change

`website/src/pages/community-platform.astro` — add a "Get started now →" CTA
in the closing section (alongside/near the existing "Talk to us" Calendly
link), linking to the absolute URL:

```
https://habitat.network/habitat/community/create
```

This matches the existing hardcoded-absolute-URL convention already used for
the docs link in `website/src/components/Header.astro:16`
(`https://habitat.network/habitat/api`).

## Frontend route

New file: `frontend/src/routes/community/create.tsx`, registered via
TanStack Router's file-based routing (adds `/community/create` to
`frontend/src/routeTree.gen.ts` on next generate). Unauthenticated route, no
`validateSearch` needed (no invite-token support for communities in this
iteration).

Unlike `org/create.tsx`'s single dense form, this route renders **one
`react-hook-form` instance split into two visual steps** via local component
state:

```ts
const [step, setStep] = useState<1 | 2>(1);
```

Keeping one form instance (rather than two routes) means no data is lost
moving between steps, and it reuses org/create's established pattern of a
single `FormValues` shape read/written via `watch`/`setValue`/`register`/
`Controller`.

```ts
interface FormValues {
  name: string;
  handle_subdomain: string;
  handle_subdomain_touched: boolean; // true once user manually edits the subdomain field
  contact_email: string;
  login_method: "atproto" | "google";
  login_id: string;
  use_custom_instance: boolean;
  custom_domain: string;
}
```

### Step 1 — Identity

Fields, in order:

1. **Community name** — text input, placeholder "My Community".
2. **Handle subdomain** — text input. Auto-populated live from the name via a
   new `slugifyHandle(name: string): string` util (lowercase, strip
   whitespace, strip characters that aren't valid in a handle segment — mirror
   whatever `internal/hive/hive.go`'s handle validation accepts). Auto-sync
   only happens while `handle_subdomain_touched` is false; the first manual
   edit to this field sets it to true and permanently stops overwriting the
   field from the name (standard "slug follows title until you touch it"
   UX — same pattern as e.g. GitHub repo name → URL slug).
3. **Your email address** — text input (the org's contact address). Also
   reused as `login_id` if the user later picks Google sign-in in step 2.

"Continue" button — validates name and email are non-empty, sets `step = 2`.
No network calls happen in step 1.

### Step 2 — Sign-in & hosting

1. **"How do you want members to sign in?"** — `ToggleGroup` with two options:
   AT Protocol / Google (no password/local-account option — communities skip
   that login method entirely, unlike org/create's three-way toggle).
   - AT Protocol selected → reveals `SingleHandleCombobox` (same component
     org/create uses) bound to `login_id`, letting the admin search/select an
     AT Proto handle.
   - Google selected → no additional input is shown; `login_id` is set from
     `contact_email` (step 1) directly, displayed read-only for confirmation.
2. **"How do you want your community hosted?"** — `ToggleGroup` with two
   options: Habitat Managed (default-selected, labeled "Recommended") /
   Custom.
   - Custom selected → reveals a `custom_domain` text input and mirrors
     org/create's live lookup: an unauthenticated
     `network.habitat.instance.describeInstance` query against that domain,
     showing the remote instance's display name inline or an error ("Could
     not reach that instance.").
   - Habitat Managed → target domain is `__HABITAT_DOMAIN__` (same compile-time
     constant org/create uses), no extra UI.
3. **"Back"** link/button — returns to step 1 without clearing any field.
4. **"Create community"** submit button — calls
   `procedure("network.habitat.org.create", ...)` with:
   ```ts
   {
     name: values.name,
     handle_subdomain: values.handle_subdomain,
     login_method: values.login_method, // "atproto" | "google"
     login_id: values.login_id,
     admin_handle: "admin", // fixed; changing it later is a separate future flow
   }
   ```
   targeting the resolved domain (managed constant, or `custom_domain`),
   using the same `{ domain, unauthenticated: true }` mechanism org/create
   uses today.

   On success, navigate to `/oauth-login?handle=admin`, same as org/create.
   On error, map `identity.ErrInvalidHandle` / `ErrOrgAlreadyExists` responses
   to inline field errors the same way org/create does today (no new backend
   error handling needed since this hits the same handler).

## Explicitly out of scope

- No new lexicon, no backend changes — reuses `network.habitat.org.create`
  and `network.habitat.instance.describeInstance` as-is.
- No password/local-account sign-in option for communities.
- No editable `admin_handle` field — hardcoded to `"admin"`; a separate future
  flow will handle changing it post-creation.
- No invite-token support in this flow (org/create's `?token=` search param
  and its invite-only gating are not carried over).
