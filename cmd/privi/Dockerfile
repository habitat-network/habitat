#### BUILD STAGE
#### Builds the Go binary.

FROM golang:1.25.4-alpine AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
COPY cmd/privi/go.mod cmd/privi/go.sum ./cmd/privi/


# Download dependencies
WORKDIR /app/cmd/privi
RUN go mod download

# Copy the entire repository (needed for replace directive)
COPY . /app/

# Build the binary
RUN go build -o /app/bin/privi .

FROM alpine:latest

WORKDIR /app

# Copy the binary from build stage
COPY --from=build /app/bin/privi /app/privi

# Run the binary
CMD ["/app/privi"]

