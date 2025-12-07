# DID Generator

A command-line tool to generate DID (Decentralized Identifier) keypairs and DID documents for testing with ATProto/Bluesky.

## Overview

This tool generates a DID keypair and outputs:
1. A `did.json` file containing a DID document in the `did:web` format
2. A pretty-printed summary to stdout with all the key information

The DID document can be used for testing authentication flows, particularly with PDS (Personal Data Server) instances.

## Usage

```bash
go run main.go --pds-url <PDS_URL> --did-host <DID_HOST> [--output-dir <DIR>]
```

### Flags

- `--pds-url` (required): The PDS service endpoint URL (e.g., `https://pds.example.com`)
- `--did-host` (required): Hostname (with optional port) for the `did:web` identifier
  - Simple hostname: `example.com` → `did:web:example.com`
  - Host with port: `example.com:8443` → `did:web:example.com%3A8443` (port is percent-encoded)
- `--output` (optional): Path to the output DID document file (defaults to `did.json` in current directory)

### Example

```bash
# Generate DID for a local Docker environment
go run main.go \
  --pds-url https://pds.example.com \
  --did-host host.docker.internal:8443 \
  --output ./fixtures/my-did.json

# Or use the default output path (did.json in current directory)
go run main.go \
  --pds-url https://pds.example.com \
  --did-host example.com
```

Output:
```
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
  DID Keypair Generated
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

DID:
  did:web:host.docker.internal%3A8443

Public Key (Multibase):
  zQ3shqF2mAgN1Pc3jguESKWTiuK7ZbASLHApqnXWWZZYaCr5h

Private Key (Hex):
  152cd3baabe109817b588b7f1f2819852e9e3a3b437dd6f11b32060315f70365

PDS Service Endpoint:
  https://pds.example.com

DID Document Location:
  /absolute/path/to/fixtures/my-did.json

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
⚠️  Keep the private key secure - do not commit to version control!
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
```

## Output

### DID Document (file)
Written to the path specified by `--output`. Contains the DID document in W3C DID format with:
- The DID identifier (`did:web:<host>`)
- Verification methods (public key in Multikey format)
- Service endpoints (pointing to the PDS)

### Summary (stdout)
A pretty-printed summary containing:
- The DID identifier
- The public key (multibase-encoded)
- The private key (hex-encoded)
- The PDS service endpoint
- The DID document file location

**⚠️ Security Note:** The private key is displayed in the output. Handle it carefully and never commit it to version control!

## Integration with Tests

The generated DID document can be used in integration tests. The integration tests currently generate DIDs dynamically (see `generateDIDKeyPair()` in `privi_integration_test.go`), but you can also pre-generate fixtures for specific test scenarios:

```bash
# Generate DID document for testing
cd cmd/didgen
go run main.go \
  --pds-url https://pds.example.com \
  --did-host host.docker.internal:8443 \
  --output ../../integration/fixtures/test-did.json
```

The output will show you the private key which you can manually save if needed for your test fixtures.

## Key Format

This tool uses the `secp256k1` elliptic curve (the same as used in Bitcoin and Ethereum) with the following encoding:
- **Public Key**: Multibase-encoded with multicodec prefix (format: `z...`)
- **Private Key**: Hex-encoded bytes

This matches the ATProto specification for `did:web` identifiers.

## DID Web Encoding

The tool follows the [did:web specification](https://w3c-ccg.github.io/did-method-web/) for encoding:

1. **Simple hostnames**: No special encoding needed
   - Input: `example.com`
   - Output: `did:web:example.com`
   - Resolves to: `https://example.com/.well-known/did.json`

2. **Ports are percent-encoded**: A colon before a port number is encoded as `%3A`
   - Input: `example.com:8443`
   - Output: `did:web:example.com%3A8443`
   - Resolves to: `https://example.com:8443/.well-known/did.json`
