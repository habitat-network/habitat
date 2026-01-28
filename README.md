# Habiat

## External Tools

To run all the code and tooling in this repository, you will need to have the following installe:

- GNU Make > v4 (this should be available through xcode)
- Docker Engine and the Docker CLI: https://docs.docker.com/engine/install/
- golangci-lint: https://golangci-lint.run/usage/install/
- go-test-coverage: Run `go install github.com/vladopajic/go-test-coverage/v2@latest`
- pnpm: https://pnpm.io/installation
- NodeJS: https://nodejs.org/en/download/package-manager


### Configuration for Local Development

Need dev.env with TS_AUTHKEY and TS_DOMAIN populated.
You can get this from tailscale admin page. The first time you run, there will be a prompt taking you to a link to generate an auth key.

TODO: how to get domain on first time setup?

## Local Development

We use moon to manage dependencies and builds.

Running `moon frontend:dev` should be sufficient because frontend is the top-most dependency.
If funnel fails to build, do `moon funnel:build`

## Testing

To run all unit tests, run:

```
make test
```

There is also a make-rule for getting test coverage, which will open a file in your browser showing the coverage information for various files:

```
make test-coverage
```
