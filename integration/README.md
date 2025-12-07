# Integration Tests

This directory contains integration tests for the Privi service using Testcontainers.

## Overview

The integration tests spin up:
1. **Privi container** - Running with HTTPS enabled (self-signed certificates)
2. **PDS container** - Bluesky PDS that requires HTTPS communication with Privi

## Prerequisites

- Docker installed and running
- Go 1.25.4 or later

**IMPORTANT**: Make sure Docker is running before executing the tests. You can verify with:
```bash
docker info
```

If Docker is not running, start it with:
```bash
# On macOS
open -a Docker

# Or use Docker Desktop UI
```

## Running the Tests

```bash
cd integration
go test -v
```

To skip integration tests (e.g., in CI without Docker):

```bash
go test -short
```

## Test Structure

### `testcontainers.go`
Contains the infrastructure code for managing test containers:
- `TestEnvironment` - Manages the lifecycle of all test containers
- `generateSelfSignedCert()` - Creates self-signed certificates for HTTPS
- `startPriviContainer()` - Starts the Privi service with HTTPS enabled
- `startPDSContainer()` - Starts the Bluesky PDS service

### `privi_integration_test.go`
Contains the actual integration tests:
- `TestPriviOAuthFlow` - Tests the complete OAuth flow between Privi (as OAuth server) and PDS
  - Creates a test user account on the PDS
  - Initiates OAuth authorization request to Privi
  - Verifies Privi redirects to PDS for authentication
  - Tests the OAuth flow as described in internal/oauthserver/README.md
- `TestPriviClientMetadata` - Tests Privi's OAuth client metadata endpoint
  - Verifies client_id, grant_types, redirect_uris, and other OAuth metadata
- `TestPriviDIDDocument` - Tests Privi's DID document endpoint
  - Verifies the did:web document structure and service endpoints

## HTTPS Setup

The tests automatically generate self-signed certificates for HTTPS communication using the [tlscert](https://github.com/mdelapenya/tlscert) library:
- Certificates are created in a temporary directory via `tlscert.SelfSignedFromRequest()`
- Mounted into the Privi container at `/certs/`
- Cleaned up automatically after tests complete

## Container Configuration

### Privi Container
- Built from `cmd/privi/Dockerfile`
- Exposed on port 8443 (HTTPS)
- Uses SQLite database at `/data/repo.db`
- JWT key stored at `/data/key.jwk`

### PDS Container
- Uses official Bluesky PDS image: `ghcr.io/bluesky-social/pds:latest`
- Exposed on port 3000 (HTTP)
- Configured to communicate with Privi service

## OAuth Flow Testing

The integration tests implement the OAuth authentication flow as described in `internal/oauthserver/README.md`:

1. **App initiates authorization**: Test simulates a client app calling `/oauth/authorize` with:
   - Client ID (Privi's client metadata URL)
   - Redirect URI (mock client callback)
   - PKCE parameters (code_challenge, code_challenge_method)
   - User handle (test account on PDS)

2. **Privi begins PDS OAuth Flow**: Privi:
   - Resolves the user's handle to their DID
   - Creates DPoP key for secure token binding
   - Redirects user to PDS authorization endpoint
   - Stores OAuth state in session cookie

3. **User authenticates with PDS**: Test creates a PDS account and verifies redirect

4. **PDS redirects to Privi callback**: After authentication, PDS would redirect to `/oauth-callback`

5. **Privi completes flow**: Privi exchanges authorization code for PDS tokens and issues client tokens

The tests verify each step of this flow, including proper redirects, session management, and OAuth metadata.

## DID Generation Helper

For testing DID-based authentication, you can use the `didgen` tool to generate DID keypairs and documents:

```bash
cd ../cmd/didgen
go run main.go \
  --pds-url http://pds.example.com:3000 \
  --did-host sashankg.github.io \
  --output ./fixtures/test-did.json
```

This generates a DID document file and outputs a summary with:
- DID identifier (properly encoded per did:web spec)
- Public key (multibase format)
- Private key (hex format)
- DID document file location

The integration tests have the DID generation logic built-in (see `generateDIDKeyPair()` in `privi_integration_test.go`), but the standalone script is useful for:
- Pre-generating test fixtures
- Manual testing with external services
- Understanding the DID document structure

**Important for Integration Tests**: The DID document's `serviceEndpoint` must be set to `http://pds.example.com:3000` because:
- Both Privi and PDS run in Docker containers on a shared network
- PDS has the network alias `pds.example.com` and listens on port `3000`
- Privi (running in Docker) uses this hostname to reach the PDS for OAuth flows
- The test host accesses PDS via `env.PDSURL` (e.g., `http://localhost:32829`)

See `../cmd/didgen/README.md` for more details.

## Adding New Tests

To add new integration tests:

1. Add test functions to `privi_integration_test.go`
2. Use the `TestEnvironment` to get container URLs
3. Mark tests with `if testing.Short() { t.Skip() }` to allow skipping
4. Use the `require` package for setup assertions
5. Use the `assert` package for test assertions

Example:

```go
func TestNewFeature(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    ctx := context.Background()
    env, err := NewTestEnvironment(ctx)
    require.NoError(t, err)
    defer env.Cleanup()

    // Your test code here
    client := &http.Client{
        Transport: &http.Transport{
            TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
        },
    }

    resp, err := client.Get(env.PriviURL + "/your-endpoint")
    require.NoError(t, err)
    defer resp.Body.Close()

    assert.Equal(t, http.StatusOK, resp.StatusCode)
}
```

## Troubleshooting

### Docker Issues
- Ensure Docker daemon is running
- Check Docker has enough resources allocated
- Verify you have permission to access Docker socket

**Colima Users:**
If you're using Colima instead of Docker Desktop, you may need to set the `DOCKER_HOST` environment variable:
```bash
export DOCKER_HOST=unix://$HOME/.colima/default/docker.sock
go test -v
```

If you encounter I/O errors or corrupted storage:
```bash
# Stop Colima
colima stop

# Clean up Docker resources
colima delete

# Restart with fresh storage
colima start --cpu 4 --memory 8

# Set the Docker host
export DOCKER_HOST=unix://$HOME/.colima/default/docker.sock
```

### Disk Space Issues
- Integration tests build Docker images which require disk space
- Clean up unused Docker resources: `docker system prune -a`
- For Colima: Increase disk size when starting: `colima start --disk 100`

### Certificate Issues
- Self-signed certificates are generated automatically using `tlscert` library
- Tests use `InsecureSkipVerify: true` for TLS client config
- Certificates are stored in temporary directory and cleaned up after tests

### Container Startup Timeouts
- PDS container may take up to 60 seconds to start
- Privi container should start within 30 seconds
- Increase timeout values in `testcontainers.go` if needed

### Port Conflicts
- Testcontainers automatically assigns random host ports
- No manual port configuration needed
- Use `env.PriviURL` and `env.PDSURL` to get actual URLs
