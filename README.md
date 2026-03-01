# Habiat

## Environement Setup

All external tools are managed by [proto](https://moonrepo.dev/docs/proto) which installs the correct versions declared in [.prototools](.prototools). To get setup, run:

```
bash <(curl -fsSL https://moonrepo.dev/install/proto.sh)
proto install
```

### Moonrepo

We use [moonrepo](https://moonrepo.dev/docs) to manage our monorepo. A crash course is available in [.moon/README.md](.moon/README.md).

## Local Development

`moon :dev` tasks read environment variables from `dev.env` which is gitignored.

### Tailscale Funnel

We use Tailscale Funnel to make local services reachable from the public internet. 
The following environment variables are required: 
- TS_AUTHKEY (can be generate [here](https://login.tailscale.com/admin/settings/keys))
- TS_DOMAIN (your Tailnet DNS name found [here](https://login.tailscale.com/admin/dns)) populated.

### Running Habitat frontend

The following command will spin up the primary frontend and backend services:

```
moon frontend:dev
```
