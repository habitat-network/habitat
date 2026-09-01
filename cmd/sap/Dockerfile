# syntax=docker/dockerfile:1

#### BUILD STAGE
#### Builds the Go binary. sap has no embedded frontend (unlike cmd/pear),
#### so this is a single stage.

FROM golang:1.27-alpine AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY --parents go.work go.work.sum \
		 go.mod go.sum \
		 */go.mod */go.sum \
		 cmd/*/go.mod cmd/*/go.sum ./

WORKDIR /app/cmd/sap
RUN go mod download

COPY . /app/

RUN go build -o /app/bin/sap .

FROM alpine:latest

WORKDIR /app

COPY --from=build /app/bin/sap /app/sap

CMD ["/app/sap"]
