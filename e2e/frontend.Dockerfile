# syntax=docker/dockerfile:1

# The Habitat web client (frontend/) as a static site, for the e2e test bed (see
# compose.yaml). It is built for the origin the test bed serves it at, because
# the origin is baked into the OAuth client metadata and redirect URIs.
# Build context is the repo root.

FROM node:26-slim AS build
WORKDIR /app
RUN npm install -g pnpm@11.5.1

# Manifests first so the install layer stays cached when sources change.
COPY --parents pnpm-lock.yaml pnpm-workspace.yaml package.json \
	frontend/package.json \
	typescript/internal/package.json \
	typescript/api/package.json \
	typescript/habitat/package.json ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
	pnpm install --frozen-lockfile --filter frontend...

COPY --parents tsconfig.options.json typescript/globals.d.ts \
	typescript/vite.config.app.ts \
	frontend typescript/internal typescript/api typescript/habitat ./

ARG APP_ORIGIN=https://app.habitat-e2e.dev
ARG HABITAT_DOMAIN=habitat-e2e.dev
ENV VITE_BASE_URL=$APP_ORIGIN VITE_HABITAT_DOMAIN=$HABITAT_DOMAIN OUT_DIR=/dist
# Skips `tsc --build`: typechecking is its own task and not what this image is for.
RUN pnpm --filter frontend exec vite build

FROM caddy:2-alpine
COPY --from=build /dist /srv
# Single-page app: unknown paths serve index.html.
RUN printf ':80 {\n\troot * /srv\n\ttry_files {path} /index.html\n\tfile_server\n}\n' > /etc/caddy/Caddyfile
