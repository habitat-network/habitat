# Habitat

⚠️ This repository is under active development and may introduce breaking changes. Please reach out on Bluesky @habitat.network for up-to-date information and any usage questions. ⚠️

## Environment Setup

All external tools are managed by [proto](https://moonrepo.dev/docs/proto) which installs the correct versions declared in [.prototools](.prototools). To get setup, run:

```
bash <(curl -fsSL https://moonrepo.dev/install/proto.sh)
proto install
```

### Moonrepo

We use [moonrepo](https://moonrepo.dev/docs) to manage our monorepo. A crash course is available in [.moon/README.md](.moon/README.md).

## Local Development

The whole dev stack runs under Docker Compose (root `docker-compose.yml`), so
you only need Docker — not local Caddy or Meilisearch installs. Source is
bind-mounted into the containers, so hot reload works as before. Postgres,
Meilisearch and Caddy have no Compose profile and always start; app dev servers
are grouped into profiles, so a `moon ...:dev` task only brings up the services
it actually needs.

Copy [dev.env.example](dev.env.example) to `dev.env` (gitignored) and fill in
your values. `moon :dev` tasks read it too.

```
moon frontend:dev   # pear + frontend (+ infra, ngrok, pear-pages)
moon chalk:dev      # pear + chalk (+ infra, pear-pages) — no frontend/sap/etc.
moon sap:dev
moon root:dev       # backing services only
moon root:logs      # follow all container logs
moon root:down      # stop this worktree's stack
```

Caddy reuses the host's CA at `~/Library/Application Support/Caddy` (mounted at
`/data/caddy`, Caddy's data dir in the image). Certificates are therefore
already trusted by the host browser, and every worktree's Caddy issues certs
from the same CA. Worker containers mount the CA directory read-only and point
`NODE_EXTRA_CA_CERTS` at it. The host CA must already be trusted in the macOS
keychain (it is if you've used local Caddy before); otherwise install its
`pki/authorities/local/root.crt` once.

### Multiple worktrees / branches

Compose derives its project name from the worktree directory, so each worktree
gets its own containers and volumes automatically (set `COMPOSE_PROJECT_NAME`
in `dev.env` if two worktrees happen to share a directory name). Only Caddy
publishes a host port, configured with `HABITAT_DEV_PORT` in `dev.env`:

- unset/`443` → `https://frontend.local.habitat.network` (the default)
- e.g. `8443` → `https://frontend.local.habitat.network:8443`

Stop a stack with `moon root:down`. The `--volume` flag is not used, so data is
kept between restarts.

Note: `HABITAT_DOMAIN` stays a bare hostname because the Go `did:web` resolver
cannot handle ports in DID identifiers. Consequently, on a non-default port,
DID-based service discovery still points at port 443; for auth-heavy work use
the default `:443` instance (or run only one stack at a time).

### Ngrok

We use ngrok to make pear reachable from the public internet to support flows like PDS OAuth.
Sign up for ngrok and follow the instructions at https://dashboard.ngrok.com/get-started/setup.
Set `NGROK_DOMAIN` and `NGROK_AUTHTOKEN` in dev.env. Each worktree needs its own
reserved domain. Ngrok runs as a Compose container.
