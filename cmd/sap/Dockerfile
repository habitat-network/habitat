#### BUILD STAGE
FROM golang:1.26-alpine AS build
WORKDIR /app

# Copy go module files first for better layer caching
COPY go.mod go.sum* ./
COPY cmd/sap/go.mod cmd/sap/go.sum ./cmd/sap/

WORKDIR /app/cmd/sap
RUN go mod download

# Copy the entire repository (needed for the replace directive)
COPY . /app/

RUN go build -o /app/bin/sap .

FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates
COPY --from=build /app/bin/sap /app/sap
CMD ["/app/sap"]
