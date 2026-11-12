# AGENTS.md

Habitat is a data ownership layer for organizations, built on AT Protocol (https://atproto.com): identity management, granular access control, and admin authorization. It is a Go + TypeScript monorepo managed by [Moon](https://moonrepo.dev/).

## Working rules
- Do what has been asked; nothing more, nothing less.
- Read a file before editing it. Don't create files unless they are necessary, and never put working files, notes, or tests in the repo root.
- Never commit secrets, credentials, or `.env` files.
- Keep existing comments unless they are TODOs you are solving. Add comments on complex code.
- When renaming names or signatures across files, write a sed script rather than editing each occurrence.
- Never hand-edit generated code (`api/habitat`, `typescript/api`, the OpenAPI spec). Change `lexicons/` and run `moon run :generate`.

## Commands
Tool versions come from [Proto](https://moonrepo.dev/proto) (`.prototools`); Moon is the task runner.

```bash
moon run frontend:dev      # frontend + pear:dev
moon run pear:dev          # pear with hot reload (+ ngrok, Caddy)
moon run sap:dev           # sap in dev
moon run :build            # build everything
moon run :generate         # regenerate lexicon-derived code
moon run :test             # all tests (Go uses testcontainers for Postgres)
moon run <project>:lint    # lint a project
moon run <project>:format  # format a project
```

CI runs `moon ci` on pushes to main and on PRs. Local config lives in the gitignored `dev.env`; the variable names are read in `cmd/` and `internal/` (Postgres, OAuth secrets, Google sign-in, Meilisearch, embedder). Generate secrets with `cmd/keygen`. `pear:dev` needs `NGROK_DOMAIN` for PDS OAuth redirects, and `Caddyfile` serves apps at `*.local.habitat.network`.

## Repository shape
- `cmd/` — Go binaries: `pear` (main app server: auth, routing, permissions, repo ops, OAuth provider/broker, MCP server), `sap` (sync tracker for org members' repos and the firehose), plus small utilities (`keygen`, `didgen`, `lexgen`, `pwtool`).
- `internal/` — all other Go code, one package per component. Browse it for the current set; package doc comments describe each one. Notable pieces:
  - Identity/org: `hive`, `emaildomain`, `identity`, `instance`, `opensocial` (partial implementation of the [opensocial group proposal](https://tangled.org/opensocial.group/proposal/blob/main/proposal.md)). `org` is deprecated; use `opensocial`.
  - Auth: `authn`, `login`, `oauthserver` (see its README), `pdsoauth`.
  - Data and permissions: `spaces` (`network.habitat.space` records), `fgastore` (embedded OpenFGA), `perms`, `search` (Meilisearch), `syntax`.
  - Gateways: `llmgateway`, `mcpgateway`, `mcpsearch`, `forwarding`, `p2p`.
  - Infra: `pearserver` (wires the HTTP server), `db`, `encrypt`, `telemetry`, `utils`.
- `lexicons/` — source of truth for lexicons and XRPC endpoints, which generate the Go and TypeScript bindings.
- `frontend/` — React 19 SPA (TanStack Router/Query, Tailwind 4, Vite): the org/instance management plane.
- `typescript/` — shared packages (`internal` design system, generated `api`, `xrpc-openapi-gen`) and demo apps.
- `e2e/` — Playwright tests against a real `pear` (`moon run e2e:test`; not part of `moon ci`).
- `integration/` — separate Go module holding the manually run `mcp-evals` CLI.
- `api-docs/` (Docusaurus API reference), `website/` (Astro marketing site), `templates/web-app` (Moon generator), `docs/superpowers/` (design specs and plans), `infra/` (local Postgres).

## Search notes
Search is off unless `--meilisearch_url` is set. After changing index settings, the embedder, or the record projection, run `pear run reindex` (never two at once); until then an older index serves reads but rejects writes with `search.ErrLegacyIndex`. Tests use `HABITAT_TEST_MEILISEARCH_URL` if set, otherwise testcontainers.

## Conventions
Detailed, path-scoped rules live in `.claude/rules/`; read them before working in that language:
- `go-conventions.md` — layout, error handling, typing, library choices.
- `go-tests.md` — testing conventions.
- `ts-frontend.md` — frontend rules.

Highlights:
- Go 1.26, linted by `golangci-lint` (`.golangci.yml`). Use the `go-tdd` skill for new functions, methods, and interface implementations.
- Coverage thresholds are in `.testcoverage.yml` (70% package/total, 60% per file).
- TypeScript is formatted with Prettier and linted with oxlint (`.oxlintrc.json`).
