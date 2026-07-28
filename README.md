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

`moon :dev` tasks read environment variables from `dev.env` which is gitignored.

### Ngrok

We use ngrok to make pear reachable from the public internet to support flows like PDS OAuth. 
Sign up for ngrok and follow the instructions at https://dashboard.ngrok.com/get-started/setup.
Set `NGROK_DOMAIN` in dev.env to the dev domain ngrok assigns you.

### Running Habitat frontend

The following command will spin up the primary frontend and backend services:

```
moon frontend:dev
```
