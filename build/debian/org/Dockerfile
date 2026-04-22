#### BUILD STAGE
#### Builds the Go binaries. Uses Debian-based Go image for CGO support (required by SQLite).

FROM golang:1.26-bookworm AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
COPY cmd/org/go.mod cmd/org/go.sum ./cmd/org/
COPY cmd/keygen/go.mod cmd/keygen/go.sum ./cmd/keygen/

# Download dependencies
WORKDIR /app/cmd/org
RUN go mod download

WORKDIR /app/cmd/keygen
RUN go mod download

# Copy the entire repository (needed for replace directives)
COPY . /app/

# Build the org server binary
WORKDIR /app/cmd/org
RUN go build -o /app/bin/org .

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
COPY --from=build /app/bin/org /app/org
COPY --from=build /app/bin/keygen /app/keygen
COPY build/debian/org/docker-entrypoint.sh /app/docker-entrypoint.sh
RUN chmod +x /app/docker-entrypoint.sh

# /data is the persistent volume — holds the SQLite DB and auto-generated secrets
VOLUME ["/data"]

# Default the DB path to the persistent volume.
# All other flags can be set via HABITAT_* env vars (see docker-compose.yml).
ENV HABITAT_DB=/data/repo.db

ENTRYPOINT ["/app/docker-entrypoint.sh"]
