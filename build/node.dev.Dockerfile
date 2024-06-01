FROM cosmtrek/air:latest

WORKDIR /go/src/github.com/eagraf/habitat-new
ENV air_wd=/go/src/github.com/eagraf/habitat-new

# Copy in .air.toml
COPY ./config/air.node.toml /go/src/github.com/eagraf/habitat-new/.air.toml

# Install debugger
RUN go install github.com/go-delve/delve/cmd/dlv@latest

# Download Go modules
COPY ./go.mod /go/src/github.com/eagraf/habitat-new/go.mod
COPY ./go.sum /go/src/github.com/eagraf/habitat-new/go.sum
RUN go mod download

# Volume in relevant source directories needed for live reloading
RUN mkdir -p /go/src/github.com/eagraf/habitat-new/core
RUN mkdir -p /go/src/github.com/eagraf/habitat-new/cmd
RUN mkdir -p /go/src/github.com/eagraf/habitat-new/internal
RUN mkdir -p /go/src/github.com/eagraf/habitat-new/pkg

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
