# CLAUDE.md

## Repository Overview

Habitat is building a data ownership layer for organizations, built with AT Protocol primitives (https://atproto.com). This includes identity management, granular access control, and admin authorization for organizations. This is a Go + TypeScript monorepo managed by [Moon](https://moonrepo.dev/).

## Behavioral rules (always enforced)
- Do what has been asked; nothing more, nothing less
- NEVER create files unless they're absolutely necessary for achieving your goal
- NEVER save working files, text/mds, or tests to the root folder
- NEVER commit secrets, credentials, or .env files
- NEVER remove comments unless they are TODOs you are directly solving. Prefer adding comments on complx code.
- ALWAYS read a file before editing it
- When updating names or signatures, write a sed script instead of doing each change yourself.

## Key Components

**Go binaries (`cmd/`)**
- `pear` — the main application server (auth, routing, permissions, AT Protocol repo ops, OAuth provider/broker)
- `sap` — standalone sync state tracker: crawls org members' repos and subscribes to their AT Proto event firehose to discover and resync `network.habitat.space` records
- `calendarServer` — Google Calendar integration backend; orphaned since the `typescript/apps/calendar` frontend was removed (#417)
- `pwtool` — manual debugging helper for the argon2id password hashing used by `internal/org`
- `keygen`, `didgen`, `lexgen` — utilities for key, DID, and lexicon (Go bindings) generation

There is no `funnel`/Tailscale binary anymore — local-dev public reachability (needed for PDS OAuth callbacks) is handled by `ngrok` (see Commands below).

**Go packages (`internal/`)**

Identity & org:
- `hive` — identity enrollment/minting and DID management for orgs (did:web-based, org-owned identities); also signs habitat-issued service-auth JWTs
- `org` — organization membership, admin roles, invites
- `identity` — serves DID docs and host→DID mapping over HTTP
- `instance` — instance-admin login/settings server (embedded HTML templates)

Auth:
- `authn` — `Method` abstraction for authenticating inbound requests (OAuth, service-auth)
- `login` — pluggable login `Provider`s (google, password, pds) used by `org`'s login flow
- `oauthserver` — Habitat's OAuth 2.0 provider (Fosite-based); brokers between client apps and a user's PDS OAuth (see `internal/oauthserver/README.md`)
- `pdsclient` — authenticated HTTP client factory for calling a DID's AT Proto PDS (OAuth + DPoP)
- `pdscred` — encrypted storage of PDS OAuth credentials per DID

Data & permissions:
- `pear` — permission-enforcing AT Protocol repo wrapper; historically the core abstraction, but marked for removal in favor of `internal/server` composing `internal/repo` + `internal/permissions` + `internal/spaces` directly (see exclusions in `.testcoverage.yml`) — avoid building new functionality on it
- `repo` — AT Protocol record/blob CRUD against the local store
- `permissions` — ACL store (grants to DIDs and cliques)
- `clique` — user groups (`network.habitat.clique`) usable as permission grantees
- `spaces` — `network.habitat.space` abstraction grouping records, backed by `fgastore`
- `fgastore` — Zanzibar-style relationship-based access control store wrapping an embedded OpenFGA server
- `syntax` — Habitat-specific syntax types/parsers (Habitat URI, Space URI/Key, Clique ref) extending `atproto/syntax`

Sync & networking:
- `events` — append-only event store/stream backing space sync
- `sync` — SSE endpoint for subscribing to space events
- `p2p` — libp2p-based peer registry/gossip for cross-instance data requests
- `forwarding` — AT Proto service proxying (`Atproto-Proxy` header) and PDS request forwarding using service-auth JWTs

Shared/infra:
- `server` — top-level Pear HTTP server wiring auth, org, oauthserver, permissions, repo
- `db` — generic `Store[T]` tx-scoping interface shared by GORM-backed stores
- `encrypt` — CBOR + NaCl secretbox encryption helpers
- `error` — shared sentinel errors
- `telemetry` — OpenTelemetry setup (traces/metrics/logs via OTLP)
- `types` — small cross-package shared types
- `utils` — misc HTTP/env/bsky helpers

**Frontend (`frontend/`)**
- React 19 SPA using TanStack Router + TanStack Query, Tailwind CSS 4, Vite
- This is the org/instance management plane: org create/join, permissions (by lexicon collection and by person), spaces, data/collection browser, onboarding, OAuth login

**TypeScript packages (`typescript/`)**
- `internal/` — shared design system, UI components, auth utilities
- `api/` — generated AT Protocol client bindings from `lexicons/` via `@atproto/lex-cli` (do not edit manually)
- `xrpc-openapi-gen` — generates an OpenAPI spec from `lexicons/` for `api-docs`
- `apps/docs` — collaborative document-editing demo app

`apps/calendar` and `apps/greensky` (calendar and Bluesky-style social feed demo apps) were removed as unused/out-of-date (#417). `cmd/calendarServer` (Google Calendar backend) still exists but is currently orphaned — nothing depends on it.

**Other top-level dirs**
- `lexicons/` — all lexicon + XRPC endpoint definitions (AT Protocol schemas); source of truth for both `api/habitat` (Go, via `lexgen`) and `typescript/api` (TS, via `@atproto/lex-cli`)
- `api-docs/` — Docusaurus site rendering the generated OpenAPI spec as API reference docs
- `website/` — marketing site (Astro); not Moon-managed, pnpm workspace member only
- `templates/web-app` — Moon generator template for scaffolding new TanStack/Vite apps (`moon generate web-app`)
- `integration/` — separate Go module with end-to-end tests against a live `pear` instance via testcontainers; runs only via manual `workflow_dispatch` (not part of `moon ci`)
- `docs/superpowers/` — in-progress design specs and plans
- `infra/docker-compose.yml` — local Postgres for development

## Local development domains

`Caddyfile` reverse-proxies `*.local.habitat.network` subdomains to each app's dev port (frontend, docs, pear, sap) with locally-trusted TLS. `pear:dev` also runs `ngrok` to expose the local server publicly under `NGROK_DOMAIN`, which is required for PDS OAuth redirect flows.

## Commands

Tool versions are managed by [Proto](https://moonrepo.dev/proto) via `.prototools`. Moon is the primary task runner.

```bash
# Development
moon :dev-all           # start all apps in dev (frontend + backend, docs)
moon frontend:dev       # frontend only (habitat management plane) + pear backend
moon pear:dev           # Pear server with Air hot reload (+ ngrok, + Caddy)
moon sap:dev            # sap sync service in dev

# Build
moon :build             # build everything
moon pear:build         # build Pear binary → bin/pear
moon typescript:build   # build all TS packages

# Generate (lexicon-derived code — never hand-edit outputs)
moon :generate          # regenerate api/habitat, typescript/api, openapi spec, api-docs

# Test
moon :test              # all tests (Go uses testcontainers for PostgreSQL isolation)
go test ./...           # Go tests directly
moon integration:test   # end-to-end tests against a live pear instance (manual/CI workflow_dispatch only)

# Lint / Format
moon :lint-check        # lint all projects
moon :format            # format all projects (runs on staged files in pre-commit hook)
golangci-lint run       # Go linting
```

**Environment** — in `dev.env` (gitignored):
- `HABITAT_PGURL` — PostgreSQL connection string (defaults to a local SQLite file otherwise)
- `HABITAT_DOMAIN`, `HABITAT_FRONTEND_DOMAIN`, `DOMAIN` — service domains
- `NGROK_DOMAIN` — public domain ngrok assigns for `pear:dev`, needed for PDS OAuth
- `SAP_SECRET` — signing secret for the `sap` service
- `HABITAT_OAUTH_SERVER_SECRET`, `HABITAT_OAUTH_CLIENT_SECRET`, `HABITAT_PDS_CRED_ENCRYPT_KEY` — OAuth/encryption secrets (generate with `cmd/keygen`)
- `HABITAT_GOOGLE_CLIENT_ID` / `HABITAT_GOOGLE_CLIENT_SECRET` — Google Sign-In login method
- `HABITAT_ADMIN_PASSWORD` — preset instance admin password (random + printed once if unset)

## Code conventions

Detailed, automatically-enforced conventions live in `.claude/rules/`: `go-conventions.md` (project layout, error handling, typing, library preferences), `go-tests.md` (testing conventions), and `ts-frontend.md` (frontend-scoped rules). Treat those as the authoritative source; the notes below are a quick summary.

**Go**
- Go 1.26. Standard layout: entrypoints in `cmd/`, everything else in `internal/`.
- Linter: `golangci-lint` (config: `.golangci.yml`), `bodyclose` + `sloglint` enabled
- No generated files edited manually; run `moon :generate` to regenerate
- Coverage thresholds: 70% package/total, 60% per file (see `.testcoverage.yml`); exclusions include `cmd/`, mocks, `internal/telemetry`, `internal/utils`, `internal/oauthserver`, most `server.go` pass-throughs, and `internal/pear` (slated for removal)
- Use the `go-tdd` skill for new functions/methods/interface implementations

**TypeScript / Frontend**
- Formatter: Prettier 3.6.2; Linter: oxlint 1.28.0 (config: `.oxlintrc.json`)
- `typescript/api/` is fully generated — never edit files there directly

**General**
- Monorepo tasks defined in `.moon/workspace.yml` and per-project `moon.yml`
- CI runs `moon ci` on push to master and on PRs
