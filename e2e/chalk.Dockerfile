# syntax=docker/dockerfile:1

# The Habitat app (typescript/apps/app, the unified chalk/drop/frontend app) for
# the e2e test bed (see compose.yaml). It is a Cloudflare Worker, so the image
# builds it and serves the build with `vite preview`, which runs the worker in
# workerd with local D1 and Durable Objects. Build context is the repo root.
# The root tsconfig.json is deliberately not copied: it references every
# workspace package, and the build would need them all installed.

FROM node:26-slim
WORKDIR /app
RUN npm install -g pnpm@11.5.1

# Manifests first so the install layer stays cached when sources change.
COPY --parents pnpm-lock.yaml pnpm-workspace.yaml package.json \
	typescript/apps/app/package.json \
	typescript/internal/package.json \
	typescript/api/package.json \
	typescript/habitat/package.json ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
	pnpm install --frozen-lockfile --filter app...

COPY --parents tsconfig.options.json typescript/globals.d.ts \
	typescript/apps/app typescript/internal typescript/api typescript/habitat ./

WORKDIR /app/typescript/apps/app
# The browser resolves handles of DIDs bsky doesn't know through this pear.
ARG HABITAT_DOMAIN=habitat-e2e.dev
ENV VITE_HABITAT_DOMAIN=$HABITAT_DOMAIN
RUN pnpm exec vite build

ENV WRANGLER_SEND_METRICS=false CI=1
COPY e2e/chalk-entrypoint.sh /usr/local/bin/chalk-entrypoint
ENTRYPOINT ["chalk-entrypoint"]
