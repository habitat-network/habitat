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

## Key Components

**Go binaries (`cmd/`)**
- `pear` — the main application server (auth, routing, permissions, AT Protocol repo ops)
- `funnel` — reverse proxy/tunnel with Tailscale integration for local development
- `calendarServer` — Google Calendar integration for the `typescript/apps/calendar` app backend
- `keygen`, `didgen`, `lexgen` — utilities for key, did, lexicon generation.

**Go packages (`internal/`)**
- `pear` — permission-enforcing AT Protocol repository wrapper (core abstraction)
- `hive` — identity enrollment and DID management for orgs
- `org` — organization management
- `oauthserver` — OAuth 2.0 server (Fosite-based)
- `authn` / `login` — authentication and session management
- `permissions` — ACL system
- `pdsclient` — Personal Data Server client (AT protocol integration)
- `clique` — user groups
- `inbox` — under development, notifications between habitat (cmd/pear entrypoint) servers
- `node` — infrastructure for habitat server inter-communication

**Frontend (`frontend/`)**
- React 19 SPA using TanStack Router + TanStack Query, Tailwind CSS 4, Vite

**TypeScript packages (`typescript/`)**
- `internal/` — shared design system, UI components, auth utilities
- `api/` — generated AT Protocol client bindings (do not edit manually)
- `apps/` — calendar, docs, greensky sub-apps

**API types**
- `lexicons/` - all lexicon + XRPC endpoints (as defined in AT protocol) live here


## Commands

Tool versions are managed by [Proto](https://moonrepo.dev/proto) via `.prototools`. Moon is the primary task runner.

```bash
# Development
moon :dev-all           # start all apps in dev (frontend + backend, calendar, docs)
moon frontend:dev       # frontend only (habitat home + management plane in frontend/)
moon pear:dev           # Pear server with Air hot reload

# Build
moon :build             # build everything
moon pear:build         # build Pear binary → bin/pear
moon typescript:build   # build all TS packages

# Test
moon :test              # all tests (Go uses testcontainers for PostgreSQL isolation)
go test ./...           # Go tests directly

# Lint / Format
moon :lint-check        # lint all projects
moon :format            # format all projects (runs on staged files in pre-commit hook)
golangci-lint run       # Go linting
```


**Environment** — in `dev.env` (gitignored):
- `HABITAT_PGURL` — PostgreSQL connection string
- `HABITAT_DOMAIN`, `DOMAIN` — service domains
- `TS_AUTHKEY`, `TS_DOMAIN` — Tailscale (for Funnel)
- OAuth keys, encryption keys, signing secrets

## Code conventions

**Go**
- Linter: `golangci-lint` (config: `.golangci.yml`), `bodyclose` enabled
- No generated files edited manually; run `moon :generate` to regenerate
- Coverage thresholds: 70% package/total, 60% per file (see `.testcoverage.yml`); exclusions for `cmd/`, mocks, telemetry

**TypeScript / Frontend**
- Formatter: Prettier 3.6.2; Linter: oxlint 1.28.0 (config: `.oxlintrc.json`)
- `typescript/api/` is fully generated — never edit files there directly

**General**
- Monorepo tasks defined in `.moon/workspace.yml` and per-project `moon.yml`
- CI runs `moon ci` on push to master and on PRs
