#### UI BUILD STAGE
#### Builds the pear-pages static site that the Go binary embeds and serves
#### under /ui/ (member login + instance admin pages). The output must land in
#### internal/webui/dist so the //go:embed all:dist in internal/webui picks it up.

FROM node:26-slim AS ui
WORKDIR /app

# pnpm is the workspace package manager (see root package.json packageManager).
# Install it directly rather than via corepack, which is no longer bundled with
# recent Node releases.
RUN npm install -g pnpm@11.5.1

# Copy the full workspace; a pnpm workspace install needs every package manifest
# and the lockfile. node_modules/bin/etc. are excluded via .dockerignore.
COPY . .

# Install only pear-pages and its workspace dependencies, then build into the
# embed dir. pear-pages consumes `internal` from source, so it does not need to
# be pre-built.
RUN pnpm install --frozen-lockfile --filter pear-pages...
RUN pnpm --filter pear-pages exec vite build --outDir /app/internal/webui/dist

#### BUILD STAGE
#### Builds the Go binary.

FROM golang:1.26-alpine AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
COPY cmd/pear/go.mod cmd/pear/go.sum ./cmd/pear/


# Download dependencies
WORKDIR /app/cmd/pear
RUN go mod download

# Copy the entire repository (needed for replace directive)
COPY . /app/

# Overlay the pre-built UI so it is embedded into the binary. The committed dist
# only contains a .gitkeep, so without this the embedded /ui/ would be empty.
COPY --from=ui /app/internal/webui/dist /app/internal/webui/dist

# Build the binary
RUN go build -o /app/bin/pear .

FROM alpine:latest

WORKDIR /app

# Copy the binary from build stage
COPY --from=build /app/bin/pear /app/pear

# Run the binary
CMD ["/app/pear"]

