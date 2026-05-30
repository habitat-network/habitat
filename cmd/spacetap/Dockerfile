FROM golang:1.26-alpine AS build
RUN apk --no-cache add gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o /app/spacetap .

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /app
COPY --from=build /app/spacetap /app/spacetap
EXPOSE 8080
USER nobody
CMD ["/app/spacetap"]
