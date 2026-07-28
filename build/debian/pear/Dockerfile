#### PEAR-PAGES BUILD STAGE
#### Builds the pear-pages SPA that pear embeds and serves under /ui/.

FROM node:22-slim AS pear-pages-build
WORKDIR /app

RUN npm install -g pnpm@11.5.1

# Install deps with frozen lockfile for reproducibility
COPY pnpm-workspace.yaml pnpm-lock.yaml package.json tsconfig.options.json ./
COPY typescript/ ./typescript/
RUN pnpm install --frozen-lockfile

# Build typescript/internal (pear-pages depends on it), then pear-pages itself.
# --outDir mirrors what moon passes during local builds.
RUN pnpm --filter internal build
RUN pnpm --filter pear-pages exec vite build --outDir /app/internal/webui/dist

#### BUILD STAGE
#### Builds the Go binaries. Uses Debian-based Go image for CGO support (required by SQLite).

FROM golang:1.26-bookworm AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
COPY cmd/pear/go.mod cmd/pear/go.sum ./cmd/pear/
COPY cmd/keygen/go.mod cmd/keygen/go.sum ./cmd/keygen/

# Download dependencies
WORKDIR /app/cmd/pear
RUN go mod download

WORKDIR /app/cmd/keygen
RUN go mod download

# Copy the entire repository (needed for replace directives)
COPY . /app/

# Overwrite the empty dist placeholder with the real pear-pages build
COPY --from=pear-pages-build /app/internal/webui/dist /app/internal/webui/dist

# Build the pear server binary
WORKDIR /app/cmd/pear
RUN go build -o /app/bin/pear .

# Build the keygen binary (used in the entrypoint to auto-generate secrets)
WORKDIR /app/cmd/keygen
RUN go build -o /app/bin/keygen .

#### RUNTIME STAGE
#### Debian bookworm slim for YunoHost / Debian server compatibility.

FROM debian:bookworm-slim

WORKDIR /app

# ca-certificates is needed for outbound TLS (ATProto/Bluesky identity resolution)
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy binaries and entrypoint from build stage
COPY --from=build /app/bin/pear /app/pear
COPY --from=build /app/bin/keygen /app/keygen
COPY build/debian/pear/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# /data is the persistent volume — holds the SQLite DB and auto-generated secrets
VOLUME ["/data"]

# Default the DB path to the persistent volume.
# All other flags can be set via HABITAT_* env vars (see docker-compose.yml).
ENV HABITAT_DB=/data/repo.db

ENTRYPOINT ["/app/docker-entrypoint.sh"]
