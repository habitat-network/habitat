# Self-hosting Habitat Frontend

## Prerequisites

- A server running Debian (or any Linux distro with Docker support, e.g. YunoHost)
- [Docker](https://docs.docker.com/engine/install/debian/) installed
- Git installed (`sudo apt-get install -y git`)
- A domain pointed at your server for the frontend (e.g. `habitat.example.com`)
- A running pear server on a separate subdomain (e.g. `pear.example.com`) — see `../pear/README.md`

## Setup

**1. Clone the repo**

```bash
git clone https://github.com/habitat-network/habitat
cd habitat
```

**2. Create a `.env` file**

```bash
cd build/debian/frontend
```

```
DOMAIN=habitat.example.com
HABITAT_DOMAIN=pear.example.com
```

`DOMAIN` is where the frontend is served. `HABITAT_DOMAIN` is the domain of your pear server. Both are baked into the app at build time.

**3. Build and start**

From the `build/debian/frontend` directory:

```bash
docker compose build
docker compose up -d
```

The build takes a few minutes the first time. The frontend will be available on port 3000.

**4. Set up the reverse proxy**

Point your reverse proxy at `http://localhost:3000`. On YunoHost, install the **Redirect** app with:
- Domain: `habitat.example.com`
- Path: `/`
- Type: Reverse-proxy
- Target: `http://127.0.0.1:3000`
- Access: Visitors

## Updates
Pull the latest version of the git repo

```bash
docker compose build
docker compose up -d
```

## Configuration

| Variable | Required | Default | Description |
|---|---|---|---|
| `DOMAIN` | yes | — | Domain where the frontend is served (no `https://` prefix) |
| `HABITAT_DOMAIN` | yes | — | Domain of the pear server (no `https://` prefix) |
| `FRONTEND_PORT` | no | `3000` | Host port to expose the frontend on |
