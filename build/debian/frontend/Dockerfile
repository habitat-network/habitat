#### BUILD STAGE
#### Builds the frontend static files using pnpm workspaces.

FROM node:24.4.1-bookworm AS build

ARG DOMAIN
ARG HABITAT_DOMAIN

# Enable corepack and install pnpm
RUN corepack enable && corepack prepare pnpm@10.21.0 --activate

WORKDIR /app

# Copy workspace manifests first for better layer caching
COPY pnpm-workspace.yaml package.json pnpm-lock.yaml ./
COPY frontend/package.json ./frontend/
COPY typescript/internal/package.json ./typescript/internal/
COPY typescript/api/package.json ./typescript/api/

RUN pnpm install --frozen-lockfile

# Copy the rest of the source
COPY . .

# Build workspace dependencies in order before the frontend
RUN pnpm --filter api build
RUN pnpm --filter internal build

# Build with domains baked in and hash routing enabled
RUN DOMAIN=$DOMAIN HABITAT_DOMAIN=$HABITAT_DOMAIN HASH_ROUTING=true \
    pnpm --filter frontend build

#### RUNTIME STAGE
#### Serve static files with nginx on port 80.
#### TLS is handled upstream by YunoHost's nginx.

FROM nginx:stable-bookworm

COPY --from=build /app/frontend/dist /usr/share/nginx/html
