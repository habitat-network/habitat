# Self-hosting Pear

## Prerequisites

- A server running Debian (or any Linux distro with Docker support, e.g. YunoHost)
- [Docker](https://docs.docker.com/engine/install/debian/) installed
- A domain name pointed at your server (e.g. `pear.example.com`)

## Setup

**1. Download the compose file**

```bash
curl -O https://raw.githubusercontent.com/habitat-network/habitat/master/build/debian/pear/docker-compose.yml
```

**2. Create a `.env` file in the same directory**

```bash
HABITAT_DOMAIN=pear.example.com
```

That's the only required setting. See [Configuration](#configuration) for all options.

**3. Start the server**

```bash
docker compose up -d
```

On first run, the server will automatically generate the three cryptographic secrets it needs and save them to a persistent volume at `/data/.secrets.env`. You will see log lines like:

```
[pear] generated HABITAT_PDS_CRED_ENCRYPT_KEY and saved to /data/.secrets.env
```

These secrets are stable — they will be reloaded from the volume on every subsequent restart and will never be regenerated unless you delete the volume.

## Updates

```bash
docker compose pull && docker compose up -d
```

**4. Restarting the server**

When restarting the server, make sure to use the same Docker volume as previous runs. You can do this by running `docker ps` to see running containers, identify the pear container (typically a name like `pear-release-pear-1`), and finding that container's mounts via `docker inspect -f '{{ json .Mounts }}' <container-name>]`. You should see an entry that looks something like `[{"Type":"volume","Name":"pear-release_pear_data","Source":"/var/lib/docker/volumes/pear-release_pear_data/_data","Destination":"/data","Driver":"local","Mode":"rw","RW":true,"Propagation":""}]
`

To restart the server using the same volume with pre-existing data, run `docker compose up -d -v pear_data:/path/to/mounted/directory`, in the example above, it would be `/var/lib/docker/volumes/pear-release_pear_data/_data`.


## Configuration

All settings are configured via environment variables in your `.env` file.

| Variable | Required | Default | Description |
|---|---|---|---|
| `HABITAT_DOMAIN` | yes | — | Publicly accessible domain for your server (no `https://` prefix) |
| `HABITAT_PORT` | no | `8000` | Port the server listens on |
| `HABITAT_DB` | no | `/data/repo.db` | Path to the SQLite database file inside the container |
| `HABITAT_SERVICE_NAME` | no | `habitat` | Service name used to identify this server in ATProto DID documents |
| `HABITAT_DEBUG` | no | `false` | Enable verbose debug logging |
| `HABITAT_HTTPSCERTS` | no | — | Directory containing `fullchain.pem` and `privkey.pem`. Leave unset if TLS is handled by a reverse proxy (recommended) |
| `HABITAT_PDS_CRED_ENCRYPT_KEY` | auto-generated | — | 32-byte base64-encoded encryption key for PDS credentials |
| `HABITAT_OAUTH_SERVER_SECRET` | auto-generated | — | 32-byte base64-encoded secret for the OAuth server |
| `HABITAT_OAUTH_CLIENT_SECRET` | auto-generated | — | 32-byte base64-encoded secret for the OAuth client |

The three secrets at the bottom are generated automatically on first run. You can override them by setting them explicitly in `.env` — for example, if you are migrating an existing installation.

## Data persistence

All persistent data lives in the `pear_data` Docker volume:

| Path | Contents |
|---|---|
| `/data/repo.db` | SQLite database |
| `/data/.secrets.env` | Auto-generated secrets |

To back up your data:

```bash
docker run --rm -v pear_data:/data -v $(pwd):/backup debian:bookworm-slim \
  tar czf /backup/pear-backup.tar.gz /data
```

To restore:

```bash
docker run --rm -v pear_data:/data -v $(pwd):/backup debian:bookworm-slim \
  tar xzf /backup/pear-backup.tar.gz -C /
```

## Reverse proxy

It is recommended to run pear behind a reverse proxy (nginx, Caddy, Traefik) that handles TLS termination. Point the proxy at `localhost:8000` (or whichever port you configured) and leave `HABITAT_HTTPSCERTS` unset.

example:

```
pear.example.com {
    reverse_proxy localhost:8000
}
```


## Pushing an image for testing without merging PR
1) Get a github token for writing packages
2) docker build -f build/debian/pear/Dockerfile -t ghcr.io/habitat-network/pear:[your-test-tag-here] .
3) docker push ghcr.io/habitat-network/pear:[your-test-tag-here]
4) On the server: 
    4a) in build/debian/pear, put PEAR_TAG=[your-test-tag] in .env
    4b) docker compose pull && docker compose up -d