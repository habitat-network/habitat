# Habitat

> [!WARNING]
This repository is under active development and may introduce breaking changes. Please reach out on Bluesky @habitat.network for up-to-date information and any usage questions.

Habitat is a central hub for an organization to manage its data and connected applications. It is built on [AT Protocol](./external/atproto) and the new [permissioned data (Atproto spaces)](./external/proposals/atproto-proposals/). It implements the interoperable groups standard [./external/opensocial-proposal] so applications can parse the organization and its members, permissions, and records.

## Environment Setup

All external tools are managed by [proto](https://moonrepo.dev/docs/proto) which installs the correct versions declared in [.prototools](.prototools). To get setup, run:

```
bash <(curl -fsSL https://moonrepo.dev/install/proto.sh)
proto install
```

### Moonrepo

We use [moonrepo](https://moonrepo.dev/docs) to manage our monorepo. A crash course is available in [.moon/README.md](.moon/README.md).

## Local Development

The dev stack runs under Docker Compose (root `docker-compose.yml`), so Docker
is all you need — no local Caddy or Meilisearch installs. Source is bind-mounted
into the containers, so hot reload still works. Copy
[dev.env.example](dev.env.example) to `dev.env` (gitignored) and fill in your
values, then bring up the full stack:

```
moon root:dev       # everything: Postgres, Meilisearch, Caddy, pear, sap, frontend, workers, ngrok
moon root:logs      # follow all container logs
moon root:down      # stop the stack (data volumes are kept)
```

To work on a single app, use its `dev` task instead. Compose profiles start only
that app and its dependencies:

```
moon frontend:dev   # pear + frontend (+ backing services, pear-pages)
moon chalk:dev      # pear + chalk (+ backing services, pear-pages) — no frontend/sap/etc.
moon sap:dev
```

Caddy reuses the host CA at `~/Library/Application Support/Caddy`, so the host
browser already trusts `https://*.local.habitat.network`. Worker containers
mount the same CA for `NODE_EXTRA_CA_CERTS`, and Caddy's network aliases let
pear fetch app client metadata at those same URLs. The host CA must already be
in the macOS keychain (it is if you've used local Caddy before).

### Multiple worktrees / branches

Compose derives its project name from the worktree directory, so each worktree
gets its own containers and volumes (set `COMPOSE_PROJECT_NAME` in `dev.env` if
two worktrees share a directory name). Only Caddy publishes a host port,
configured with `HABITAT_DEV_PORT` in `dev.env`:

- unset/`443` → `https://frontend.local.habitat.network` (the default)
- e.g. `8443` → `https://frontend.local.habitat.network:8443`

Note: `HABITAT_DOMAIN` stays a bare hostname because the Go `did:web` resolver
rejects ports in DID identifiers, so on a non-default port DID-based discovery
still points at 443. Use the default `:443` instance for auth-heavy work.

### Ngrok

We use ngrok to make pear reachable from the public internet to support flows like PDS OAuth.
Sign up for ngrok and follow the instructions at https://dashboard.ngrok.com/get-started/setup.
Set `NGROK_DOMAIN` and `NGROK_AUTHTOKEN` in `dev.env` to the domain ngrok assigns
you; it runs as a Compose container. Each worktree needs its own reserved domain.
