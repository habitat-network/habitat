# Self-hosting Pear

Pear is Habitat's Organizational Data Server. It ships as a single Docker image, `ghcr.io/habitat-network/pear`, built for `linux/amd64` and `linux/arm64`.

## Releases

Every commit to `main` that passes CI is released automatically as `vYYYY.M.D-<short sha>` (for example `v2026.9.11-e3551da`). Each [GitHub release](https://github.com/habitat-network/habitat/releases) has:

- a matching image tag without the `v`, e.g. `ghcr.io/habitat-network/pear:2026.9.11-e3551da`
- a `docker-compose.yml` asset whose image tag is pinned to that release

The `latest` image tag always points at the newest release.

> Tags named `v0.0.x-testing-N` are from the mid-2024 architecture (`cmd/node`) and do not work with these instructions. Use a `vYYYY.M.D-*` release.

## Prerequisites

- A Linux server with [Docker](https://docs.docker.com/engine/install/) and Docker Compose
- A domain name pointed at your server (e.g. `pear.example.com`)
- A reverse proxy that terminates TLS (see [Reverse proxy](#reverse-proxy))

## Setup

**1. Download the compose file**

```bash
mkdir pear && cd pear
curl -LO https://github.com/habitat-network/habitat/releases/latest/download/docker-compose.yml
```

To install a specific release instead, replace `latest/download` with `download/<version>`, e.g. `download/v2026.9.11-e3551da`.

**2. Create a `.env` file in the same directory**

```bash
HABITAT_DOMAIN=pear.example.com
HABITAT_ADMIN_PASSWORD=<password for the instance admin at https://pear.example.com/admin>
```

`HABITAT_DOMAIN` is the only required setting. See [Configuration](#configuration) for all options.

**3. Start the server**

```bash
docker compose up -d
curl http://localhost:8000/health   # -> ok
```

On first run, the server automatically generates the four keys it needs and saves them to the persistent volume at `/data/.secrets.env`. You will see log lines like:

```
[pear] generated HABITAT_PDS_CRED_ENCRYPT_KEY and saved to /data/.secrets.env
```

These keys are reloaded from the volume on every restart and are never regenerated unless you delete the volume. **Back them up**: without them, stored PDS credentials cannot be decrypted, and your server's DID document changes.

## Updates

The downloaded compose file is pinned to the release you downloaded. To upgrade, download the compose file from the newer release and restart:

```bash
curl -LO https://github.com/habitat-network/habitat/releases/latest/download/docker-compose.yml
docker compose pull && docker compose up -d
```

Alternatively, set `PEAR_TAG` in `.env` to pick an image tag without re-downloading. For example, `PEAR_TAG=latest` tracks `main`, and `PEAR_TAG=2026.9.11-e3551da` pins a release. Database migrations run automatically on startup.

## Configuration

All settings are environment variables in your `.env` file. Each corresponds to a `pear` command-line flag (`HABITAT_FOO_BAR` ↔ `--foo_bar`).

| Variable | Required | Default | Description |
|---|---|---|---|
| `HABITAT_DOMAIN` | yes | — | Publicly accessible domain for your server (no `https://` prefix) |
| `HABITAT_ADMIN_PASSWORD` | no | random on every boot | Password for the `admin` user of the instance admin pages at `/admin`. If unset, a new one is printed to the logs on each start |
| `HABITAT_PORT` | no | `8000` | Port the server listens on (and is published on the host) |
| `HABITAT_DB` | no | `sqlite:///data/repo.db` | Database connection string: `sqlite:///path/to/file.db` or `postgres://…` (see [Postgres](#postgres)) |
| `HABITAT_BLOB_BUCKET` | no | `file:///data/blobs` | Blob storage: `file:///dir`, `s3://bucket?region=…`, or `gs://bucket` |
| `HABITAT_HTTPSCERTS` | no | — | Directory containing `fullchain.pem` and `privkey.pem`. Leave unset if TLS is handled by a reverse proxy (recommended) |
| `HABITAT_GOOGLE_CLIENT_ID` / `HABITAT_GOOGLE_CLIENT_SECRET` | no | — | Enable Google Sign-In as a login method. Redirect URI: `https://<HABITAT_DOMAIN>/oauth-callback` |
| `HABITAT_DEBUG` | no | `false` | Enable verbose request logging |
| `PEAR_TAG` | no | the release version | Image tag to run (see [Updates](#updates)) |
| `HABITAT_PDS_CRED_ENCRYPT_KEY` | auto-generated | — | 32-byte base64 encryption key for stored PDS credentials |
| `HABITAT_OAUTH_SERVER_SECRET` | auto-generated | — | 32-byte base64 secret for the OAuth server |
| `HABITAT_OAUTH_CLIENT_SECRET` | auto-generated | — | 32-byte base64 secret for the OAuth client |
| `HABITAT_SPACE_SIGNING_KEY` | auto-generated | — | Multibase-encoded P-256 private key this server signs space commits with; published in its DID document |

You can override the auto-generated keys by setting them in `.env`, e.g. when migrating an existing installation. To generate them yourself, run `go run ./cmd/keygen` (secrets) or `go run ./cmd/keygen -p256` (signing key) from a checkout of this repo.

The image also accepts every other `pear` flag as an environment variable; run `docker compose run --rm pear --help` to list them.

## Postgres

SQLite on the persistent volume is fine for small installations. To use Postgres instead, set a connection string:

```bash
HABITAT_DB=postgres://pear:<password>@db.example.com:5432/pear?sslmode=require
```

- The authorization store (OpenFGA) shares the same database; no extra setup is needed.
- Create the database with `UTF8` encoding (`CREATE DATABASE pear ENCODING 'UTF8'`) if you can. Pear always connects with `client_encoding=UTF8`, so other encodings work too, but UTF8 avoids surprises with non-ASCII data.
- If you connect through PgBouncer or another transaction-mode pooler, add `default_query_exec_mode=simple_protocol` to the connection string.

## Data persistence

With the defaults, all persistent data lives in the `pear_data` Docker volume:

| Path | Contents |
|---|---|
| `/data/repo.db`, `/data/repo.db.fga.db` | SQLite databases (app data and authorization store) |
| `/data/blobs/` | Uploaded blobs |
| `/data/.secrets.env` | Auto-generated keys |

Compose prefixes the volume name with the project name, which defaults to the directory name (`pear` → `pear_pear_data`). Always run `docker compose` from the same directory, or pass the same `-p <project>`, so the server reattaches to its existing volume. `docker volume ls` lists the volumes.

To back up your data (stop the server first so SQLite files are consistent):

```bash
docker compose stop
docker run --rm -v pear_pear_data:/data -v "$(pwd)":/backup debian:bookworm-slim \
  tar czf /backup/pear-backup.tar.gz /data
docker compose start
```

To restore:

```bash
docker run --rm -v pear_pear_data:/data -v "$(pwd)":/backup debian:bookworm-slim \
  tar xzf /backup/pear-backup.tar.gz -C /
```

## Reverse proxy

Run pear behind a reverse proxy (Caddy, nginx, Traefik) that handles TLS. Point the proxy at `localhost:8000` (or your `HABITAT_PORT`) and leave `HABITAT_HTTPSCERTS` unset. The server must be reachable at `https://<HABITAT_DOMAIN>`, because PDS OAuth callbacks and DID resolution depend on it.

Example `Caddyfile`:

```
pear.example.com {
    reverse_proxy localhost:8000
}
```

## Building from source

Build from a release tag rather than an arbitrary commit, so the source matches a published image:

```bash
git clone https://github.com/habitat-network/habitat && cd habitat
git checkout v2026.9.11-e3551da
docker build -f build/debian/pear/Dockerfile -t ghcr.io/habitat-network/pear:local .
```

Then set `PEAR_TAG=local` in `.env` and run `docker compose up -d` (skip `docker compose pull`).

## Pushing a test image without merging a PR

Run the [Release Pear](https://github.com/habitat-network/habitat/actions/workflows/release-pear.yml) workflow manually on your branch. It publishes `ghcr.io/habitat-network/pear:<branch-name>` (with `/` replaced by `-`) without cutting a release. Then, on the server:

1. Put `PEAR_TAG=<branch-name>` in `.env`
2. `docker compose pull && docker compose up -d`
