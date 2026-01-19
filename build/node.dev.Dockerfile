FROM cosmtrek/air:latest

WORKDIR /go/src/github.com/habitat-network/habitat
ENV air_wd=/go/src/github.com/habitat-network/habitat

# Copy in .air.toml
COPY ./config/air.node.toml /go/src/github.com/habitat-network/habitat/.air.toml

# Install debugger
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Download Go modules
COPY ./go.mod /go/src/github.com/habitat-network/habitat/go.mod
COPY ./go.sum /go/src/github.com/habitat-network/habitat/go.sum
RUN go mod download

# Volume in relevant source directories needed for live reloading
RUN mkdir -p /go/src/github.com/habitat-network/habitat/core
RUN mkdir -p /go/src/github.com/habitat-network/habitat/cmd
RUN mkdir -p /go/src/github.com/habitat-network/habitat/internal
RUN mkdir -p /go/src/github.com/habitat-network/habitat/pkg

# Optional:
# To bind to a TCP port, runtime parameters must be supplied to the docker command.
# But we can document in the Dockerfile what ports
# the application is going to listen on by default.
# https://docs.docker.com/engine/reference/builder/#expose
EXPOSE 3000
EXPOSE 3001
EXPOSE 4000
EXPOSE 80

# Live reloading
CMD [ "air" ]
