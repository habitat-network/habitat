# Test certificate

Self-signed certificate and key for `habitat-e2e.dev` and `*.habitat-e2e.dev` and `*.pds.habitat-e2e.dev` (the PDS's accounts' handles),
used by the compose test bed (`../compose.yaml`). Caddy serves it, and pear
trusts it (`SSL_CERT_FILE`) so it can fetch the web client's OAuth metadata.
The private key is public on purpose: it protects nothing and the domain is
not a real host. Valid for 100 years; regenerate with

```sh
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:prime256v1 -nodes \
  -days 36500 -keyout key.pem -out cert.pem -subj /CN=habitat-e2e.dev \
  -addext "subjectAltName=DNS:habitat-e2e.dev,DNS:*.habitat-e2e.dev,DNS:*.pds.habitat-e2e.dev" \
  -addext basicConstraints=critical,CA:TRUE
```
