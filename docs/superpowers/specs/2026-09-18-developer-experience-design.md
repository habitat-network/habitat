# Developer experience: docs reachability and agent-readable docs

Status: approved design, not yet implemented
Date: 2026-09-18

## Problem

The URL Habitat hands to developers, `/docs/`, returns 404, and the site
publishes nothing an agent can consume without scraping HTML.

The written content is in reasonable shape. `building/`, `rebac/`, and
`space-proxy/` are substantive and accurate. The failure is in routing,
canonical URLs, and machine-readable packaging.

Measured against the live site on 2026-09-17:

| URL | Status |
|---|---|
| `https://api.habitat.network/docs/` | 404 |
| `https://api.habitat.network/docs/guides/developers` | 404 |
| `https://api.habitat.network/docs/guides/developers.mdx` | 200 |
| `https://api.habitat.network/llms.txt` | 404 |
| `https://api.habitat.network/docs/api` | 200 |

## Scope

In scope: docs reachability (workstream 1) and agent-readable docs
(workstream 3).

## Workstream 1 — Docs reachability

### 1.1 Strip `.mdx` from slugs

13 of 24 hand-written pages declare a `slug:` ending in `.mdx`, so the
extension leaks into the public URL. The other 11 are correct, and the
newest directories (`rebac/`, `space-proxy/`) are among them, which
confirms this is a copy-paste artifact rather than a convention.

To be precise about the harm, because it is narrower than it first
appears: the `.mdx` URLs work. They return 200, and anything following
links from the site reaches them. What fails is the *clean* URL, so any
URL arrived at by convention rather than by following a link 404s — a
person typing it, an external site linking it, a model generating it
from priors. A URL ending in `.mdx` also reads as a source file rather
than a page wherever it is cited.

This is a correctness and polish fix. It is not what blocks agents;
§1.5 and §2.1 are.

Affected files, all at line 3:

    arch/ods.mdx              arch/overview.mdx        arch/repositories.mdx
    building/auth.mdx         building/data.mdx        building/intro.mdx
    building/pear.mdx         building/permissions.mdx
    guides/communities.mdx    guides/developers.mdx    guides/organizations.mdx
    guides/self-hosting.mdx   opensocial/intro.mdx

Apply with a sed script over `api-docs/docs`, per the repo convention of
scripting mechanical edits rather than hand-editing each file.

**Do not touch the inline links.** Links like `[Endpoints](./endpoints.mdx)`
in `space-proxy/overview.mdx:70` are the correct Docusaurus idiom: a file
reference that resolves to the target's slug at build time. They are not
affected by this bug and changing them would break them.

### 1.2 Fix the canonical domain

`docusaurus.config.ts:19` sets `url: "https://habitat.network"`, so the
generated `sitemap.xml` advertises `https://habitat.network/docs/api/...`
— a host the docs are not served on. Any crawler that trusts the sitemap
gets a dead map of the entire reference.

Set `url` to `https://api.habitat.network`. Leave `baseUrl` at `/`, which
is what the Cloudflare deployment already serves. The stale comment on
`docusaurus.config.ts:6` claiming `/habitat/api` in production should go.

### 1.3 Redirect the old URLs

Anything already linking to a `.mdx` URL — Discord, the blog, a crawler's
index — must keep working. Add `@docusaurus/plugin-client-redirects`,
a new dependency pinned to the `3.10.2` line already used by the other
Docusaurus packages.

**Generate the redirects; do not list them.** The plugin derives
redirects from the routes the site actually builds, so the rule cannot
fall out of sync with the docs. Its `lib/extensionRedirects.js` provides
`fromExtensions: ["mdx"]`, which emits a `.mdx` redirect for every route,
and a `createRedirects(path)` callback for scoping.

Use `createRedirects`, scoped to `/docs/` excluding `/docs/api/`. Of 111
built routes, 87 are generated API reference pages that never had `.mdx`
URLs; a blanket rule would emit 87 pointless redirect files. Scoping
costs one conditional and stays fully automatic.

A hand-maintained list was considered and rejected: it would need
updating whenever a page is added, which is precisely the drift this
avoids.

### 1.4 Fix the two broken references and enforce that in CI

The docs build currently succeeds while emitting two defects. Both were
confirmed by running `pnpm build` in `api-docs/` on 2026-09-18:

```
[WARNING] Docusaurus found broken links!
- Broken link on source page path = /docs/api:
   -> linking to /docs/api/habitats-api

[WARNING] Docusaurus found broken anchors!
- Broken anchor on source page path = /docs/building/permissions.mdx:
   -> linking to /docs/building/auth.mdx#OAuth
```

**The broken link.** `index.mdx:29` points at `/docs/api/habitats-api`;
the real page is `habitat-api`. This is the only "read the full API
reference" call to action on the API overview page, and it 404s.

**The broken anchor.** `building/permissions.mdx` links to
`auth.mdx#OAuth`, but `building/auth.mdx:32` is `### OAuth`, whose
generated anchor is lowercase `#oauth`. A case mismatch.

These ship because `docusaurus.config.ts:25` sets `onBrokenLinks: "warn"`,
and `onBrokenAnchors` is left at its default, which is also warn.

**CI does run the docs build.** This was checked rather than assumed:

- `moon task api-docs:build` reports `Runs in CI: Yes`.
- Its dependency `api-docs:generate` reports `Runs in CI: No`, but that
  does not matter — `api-docs`'s `build` npm script is
  `docusaurus gen-api-docs all && docusaurus build`, so it regenerates
  the reference itself regardless of the moon task.

So setting both `onBrokenLinks` and `onBrokenAnchors` to `throw` is
genuinely enforced by `moon ci`, not just locally.

**Order matters.** Fix both defects first, then flip the two settings.
Flipping first turns CI red on `main`.

**Stop the slug bug recurring.** Redirects rescue old URLs but do not
prevent a new page from shipping with a `.mdx` slug — the copy-paste that
caused this in the first place. Add a check that fails when any
`slug:` under `api-docs/docs/` ends in `.mdx`, and wire it into CI.

`api-docs/moon.yml` currently excludes the inherited `test` task
(`workspace.inheritedTasks.exclude`), so a test task must be defined
explicitly for this to run under `moon ci`. The same task runs the
`llms.txt` generator's unit tests from §2.1, so it earns its place twice.

**One gap to close as part of this.** `api-docs:build`'s inputs are
`api-docs/**/*`, `pnpm-lock.yaml`, and the `.moon` configs. The OpenAPI
spec it renders, `typescript/xrpc-openapi-gen/spec/api.json`, is not
among them. Because `moon ci` only runs affected tasks, a lexicon change
that alters the API surface will not retrigger the docs build, so a link
broken by that change is not caught. Add the spec to the task's inputs.

### 1.5 Add a real page at `/docs/`

`/docs/` is the URL being handed to developers and it 404s. There is no
index tying the sections together and no marked path through them.

Add a landing page that sequences the journey:

1. What Habitat is — link to `/habitat`
2. Five-minute hosted quickstart — link to `/space-proxy/getting-started`
3. Building on it — `building/`, `rebac/`
4. HTTP reference — `/docs/api`
5. Running your own — `/guides/self-hosting`

Step 2 matters most and is the least discoverable today.
`space-proxy/getting-started.mdx` is already a working hosted
hello-world: point an atproto OAuth client at Habitat as its identity
resolver and sign in, with no local setup. `pear.habitat.network` is
live and returns `inviteRequired: false`, and
`@habitat-network/habitat` is published on npm. Nothing links to it
from the top of the site.

### 1.6 Correct the auth section of the API overview

`index.mdx` documents authentication as `Authorization: Bearer <token>`.
The actual hosted flow is OAuth brokered through the user's PDS: Habitat
runs an OAuth server for the app while acting as an OAuth client against
the user's real PDS, as described in `space-proxy/getting-started.mdx`.

The overview page contradicts the getting-started page. A developer who
reads the overview first writes the wrong client. Rewrite the section to
describe the OAuth flow and link to the getting-started page.

## Workstream 2 — Agent-readable docs

### 2.1 `llms.txt` and `llms-full.txt`

Generate both at build time from the Docusaurus content, written into the
build output so they deploy with the site.

Generating rather than hand-writing is the point: a hand-maintained
`llms.txt` drifts from the site within weeks, and drift is worse than
absence because an agent cannot tell a stale link from a live one.

- `llms.txt` — the index: title, one-line description, and a curated
  link list grouped by section, in the order set out in 1.5.
- `llms-full.txt` — the concatenated prose of the hand-written pages.

Exclude the generated `docs/api/**` pages from `llms-full.txt`. There are
several hundred and they would swamp the prose; the OpenAPI spec at
`typescript/xrpc-openapi-gen/spec/api.json` already serves that need
better, and `llms.txt` should link to it directly.

This depends on 1.1 and 1.2: the generator reads slugs and the canonical
domain, so it must run after both are correct or it will emit the same
broken URLs in a new file.

### 2.2 A builder-facing `CLAUDE.md` template

A template an app developer copies into their own repo, so their agent
knows how to call Habitat: the base URL, the OAuth flow, how records and
collections are addressed, where the lexicons live, and links to the
canonical docs.

This is distinct from this repo's `CLAUDE.md`, which is contributor
guidance for working on Habitat itself. Conflating the two is the
mistake to avoid. Ship it as a documented, copyable page under
`building/`, not as a root-level file.


## Sequencing

Workstream 1 first, in the order above. It is self-contained, it is the
highest-leverage fix, and workstream 2 reads the slugs and canonical
domain that 1.1 and 1.2 correct.

## Verification

Every claim of completion is checked against the deployed site, not the
local build:

- The five URLs in the Problem table return their intended statuses,
  including `/docs/` and `/llms.txt` at 200.
- A sample of old `.mdx` URLs redirect rather than 404.
- `sitemap.xml` entries are all `https://api.habitat.network/...` and a
  sample resolve.
- The reference link on the API overview page resolves.
- `pnpm build` in `api-docs/` reports zero broken links and zero broken
  anchors.
- A docs build with a deliberately broken link, and one with a
  deliberately broken anchor, each fail rather than warn.
- A change to `typescript/xrpc-openapi-gen/spec/api.json` alone marks
  `api-docs:build` as affected.
