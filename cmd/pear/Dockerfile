# syntax=docker/dockerfile:1

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

# Copy the lockfile and manifests before any source so the install layer stays
# cached when unrelated files change. --frozen-lockfile only validates the
# projects the filter selects, so this needs the workspace root plus pear-pages
# and its workspace deps (internal -> api), not all 12 members.
COPY --parents pnpm-lock.yaml pnpm-workspace.yaml package.json \
	typescript/apps/pear-pages/package.json \
	typescript/internal/package.json \
	typescript/api/package.json ./

# The cache mount keeps pnpm's content-addressable store across builds, so a
# lockfile change re-links packages instead of re-downloading them.
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
	pnpm install --frozen-lockfile --filter pear-pages...

# Only the sources the build actually reads, so Go-only changes leave this whole
# stage cached. Copying the full context here would also re-expand the workspace
# back to 12 projects, and pnpm would silently redo the install unfiltered
# before running vite (it verifies node_modules against the workspace first).
COPY --parents tsconfig.options.json typescript/globals.d.ts \
	typescript/apps/pear-pages typescript/internal typescript/api ./

# pear-pages consumes `internal` (and transitively `api`) from source, so those
# do not need to be pre-built.
RUN pnpm --filter pear-pages exec vite build --outDir /app/internal/webui/dist

#### BUILD STAGE
#### Builds the Go binary.

FROM golang:1.27-alpine AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY --parents go.work go.work.sum \
		 go.mod go.sum \
		 */go.mod */go.sum \
		 cmd/*/go.mod cmd/*/go.sum ./


# Download dependencies
WORKDIR /app/cmd/pear
RUN go mod download

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
