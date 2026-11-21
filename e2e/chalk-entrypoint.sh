#!/bin/sh
# Starts the Habitat app for the e2e test bed (see compose.yaml and
# chalk.Dockerfile): writes its worker bindings, applies its D1 migrations to
# the local database, and serves the build.
set -eu

cat > .dev.vars <<VARS
APP_SESSION_SECRET=$APP_SESSION_SECRET
APP_CREDENTIALS_KEY=$APP_CREDENTIALS_KEY
APP_BASE_URL=$APP_BASE_URL
APP_HABITAT_URL=$APP_HABITAT_URL
APP_RUN_DOMAIN=$APP_RUN_DOMAIN
VARS
# `vite preview` reads the bindings next to the built worker, not the project root.
cp .dev.vars dist/server/.dev.vars

./node_modules/.bin/wrangler d1 migrations apply app --local
exec ./node_modules/.bin/vite preview --host 0.0.0.0 --port 5181
