package integration

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	toxiclient "github.com/Shopify/toxiproxy/v2/client"
	"github.com/bluesky-social/indigo/atproto/atcrypto"
	jose "github.com/go-jose/go-jose/v3"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/moby/moby/api/types/container"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// writeTLS generates a CA and a leaf cert covering the habitat test domains,
// writes ca.pem, leaf.crt, leaf.key into dir.
func writeTLS(t *testing.T, dir string) {
	t.Helper()
	caKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	caTmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "habitat-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	require.NoError(t, err)

	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	leafTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "*.local.habitat.network"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames: []string{
			"local.habitat.network",
			"*.local.habitat.network",
			"*.pear.local.habitat.network",
			"*.*.pear.local.habitat.network",
		},
	}
	leafDER, err := x509.CreateCertificate(rand.Reader, leafTmpl, caTmpl, &leafKey.PublicKey, caKey)
	require.NoError(t, err)
	leafKeyDER, err := x509.MarshalECPrivateKey(leafKey)
	require.NoError(t, err)

	write := func(name string, blocks ...*pem.Block) {
		f, err := os.Create(filepath.Join(dir, name))
		require.NoError(t, err)
		defer func() { _ = f.Close() }()
		for _, b := range blocks {
			require.NoError(t, pem.Encode(f, b))
		}
		require.NoError(t, os.Chmod(filepath.Join(dir, name), 0o644))
	}
	write("ca.pem", &pem.Block{Type: "CERTIFICATE", Bytes: caDER})
	write("leaf.crt", &pem.Block{Type: "CERTIFICATE", Bytes: leafDER})
	write("leaf.key", &pem.Block{Type: "EC PRIVATE KEY", Bytes: leafKeyDER})
}

// writeCaddyfile writes the reverse-proxy config Caddy serves TLS for pear,
// sap (with the notify path routed through toxiproxy for fault injection),
// and the test OAuth client's metadata document.
func writeCaddyfile(t *testing.T, dir string) {
	t.Helper()
	content := `{
	auto_https off
}

pear.local.habitat.network, *.pear.local.habitat.network, *.*.pear.local.habitat.network {
	tls /certs/leaf.crt /certs/leaf.key
	reverse_proxy pear:8000
}

sap.local.habitat.network {
	tls /certs/leaf.crt /certs/leaf.key
	@notify path /xrpc/network.habitat.space.notifyWrite
	reverse_proxy @notify toxiproxy:8666
	reverse_proxy sap:2580
}

testclient.local.habitat.network {
	tls /certs/leaf.crt /certs/leaf.key
	handle /client-metadata.json {
		root * /certs
		rewrite * /testclient-metadata.json
		file_server
	}
}
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Caddyfile"), []byte(content), 0o644))
}

// writeCorefile writes CoreDNS config: it answers *.local.habitat.network
// with caddyIP, and forwards everything else to the container's Docker
// embedded resolver (127.0.0.11) so service aliases (pear, sap, postgres,
// toxiproxy) still resolve.
func writeCorefile(t *testing.T, dir, caddyIP string) {
	t.Helper()
	content := fmt.Sprintf(`local.habitat.network:53 {
	template IN A {
		match ".*\\.?local\\.habitat\\.network\\.$"
		answer "{{ .Name }} 60 IN A %s"
	}
}
.:53 {
	forward . 127.0.0.11
}
`, caddyIP)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "Corefile"), []byte(content), 0o644))
}

// sapStack holds the URLs and clients later tests need to drive a live
// pear<->sap integration scenario.
type sapStack struct {
	PearBaseURL   string
	PearHTTP      string
	SapOutboxWS   string
	SapInternal   string
	Toxiproxy     *toxiclient.Client
	NotifyProxy   *toxiclient.Proxy
	SapClientID   string
	TestClientID  string
	TestClientKey *ecdsa.PrivateKey
}

// startSapStack brings up postgres, Caddy (wildcard TLS), CoreDNS (wildcard
// DNS), toxiproxy, and the pear and sap containers on one Docker network. The
// test process talks to pear and sap over their published host ports; Caddy
// is only used for in-container TLS traffic (e.g. sap's OAuth callback to
// pear, or pear's outbound calls to sap), so nothing needs host :443.
func startSapStack(ctx context.Context, t *testing.T) *sapStack {
	t.Helper()
	certsDir := t.TempDir()
	writeTLS(t, certsDir)
	writeCaddyfile(t, certsDir)

	net, err := network.New(ctx)
	require.NoError(t, err)
	t.Cleanup(func() { _ = net.Remove(ctx) })
	netName := net.Name

	// Test client + sap client IDs (metadata served by caddy / sap respectively).
	testClientID := "https://testclient.local.habitat.network/client-metadata.json"
	sapClientID := "https://sap.local.habitat.network/client-metadata.json"

	// The test client's ES256 key + served metadata document.
	testKey, testMetaJSON := buildTestClientMetadata(t, testClientID)
	require.NoError(t, os.WriteFile(filepath.Join(certsDir, "testclient-metadata.json"), testMetaJSON, 0o644))

	bindCerts := func(hc *container.HostConfig) {
		hc.Binds = append(hc.Binds, certsDir+":/certs:ro")
	}

	// 1. Caddy.
	caddy := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:              "caddy:2-alpine",
		Networks:           []string{netName},
		NetworkAliases:     map[string][]string{netName: {"caddy"}},
		Cmd:                []string{"caddy", "run", "--config", "/certs/Caddyfile", "--adapter", "caddyfile"},
		HostConfigModifier: bindCerts,
		WaitingFor:         wait.ForListeningPort("443/tcp"),
	})
	caddyIP, err := caddy.ContainerIP(ctx)
	require.NoError(t, err)

	// 2. CoreDNS (needs caddy IP).
	writeCorefile(t, certsDir, caddyIP)
	coredns := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:              "coredns/coredns:1.11.1",
		Networks:           []string{netName},
		NetworkAliases:     map[string][]string{netName: {"coredns"}},
		Cmd:                []string{"-conf", "/certs/Corefile"},
		HostConfigModifier: bindCerts,
		WaitingFor:         wait.ForListeningPort("53/tcp"),
	})
	corednsIP, err := coredns.ContainerIP(ctx)
	require.NoError(t, err)
	corednsAddr, err := netip.ParseAddr(corednsIP)
	require.NoError(t, err)
	useDNS := func(hc *container.HostConfig) { hc.DNS = []netip.Addr{corednsAddr} }

	// 3. Postgres (+ create the sap database).
	pgURL, pgHostURL := startPostgres(ctx, t, netName)

	// 4. Toxiproxy.
	toxi := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          "ghcr.io/shopify/toxiproxy:2.9.0",
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"toxiproxy"}},
		ExposedPorts:   []string{"8474/tcp"},
		WaitingFor:     wait.ForListeningPort("8474/tcp"),
	})
	toxiHost, err := toxi.PortEndpoint(ctx, "8474/tcp", "http")
	require.NoError(t, err)
	toxiClient := toxiclient.NewClient(toxiHost)

	// 5. pear.
	pEnv := pearEnv(t, pgURL, sapClientID, testClientID)
	pear := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          buildImage(ctx, t, "cmd/pear/Dockerfile", "pear:itest"),
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"pear"}},
		Env:            pEnv,
		ExposedPorts:   []string{"8000/tcp"},
		HostConfigModifier: func(hc *container.HostConfig) {
			useDNS(hc)
			bindCerts(hc)
		},
		WaitingFor: wait.ForHTTP("/health").WithPort("8000/tcp").WithStartupTimeout(90 * time.Second),
	})
	pearHTTP, err := pear.PortEndpoint(ctx, "8000/tcp", "http")
	require.NoError(t, err)

	// 6. sap.
	sap := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          buildImage(ctx, t, "cmd/sap/Dockerfile", "sap:itest"),
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"sap"}},
		Env:            sapEnv(t, pgHostURL),
		ExposedPorts:   []string{"2581/tcp"},
		HostConfigModifier: func(hc *container.HostConfig) {
			useDNS(hc)
			bindCerts(hc)
		},
		WaitingFor: wait.ForHTTP("/health").WithPort("2581/tcp").WithStartupTimeout(90 * time.Second),
	})
	sapWSHost, err := sap.PortEndpoint(ctx, "2581/tcp", "")
	require.NoError(t, err)

	// 7. Configure the notify proxy: caddy -> toxiproxy:8666 -> sap:2580.
	notifyProxy, err := toxiClient.CreateProxy("sap_notify", "0.0.0.0:8666", "sap:2580")
	require.NoError(t, err)

	return &sapStack{
		PearBaseURL:   "https://pear.local.habitat.network",
		PearHTTP:      pearHTTP,
		SapOutboxWS:   "ws://" + sapWSHost + "/channel",
		SapInternal:   "http://" + sapWSHost,
		Toxiproxy:     toxiClient,
		NotifyProxy:   notifyProxy,
		SapClientID:   sapClientID,
		TestClientID:  testClientID,
		TestClientKey: testKey,
	}
}

// buildTestClientMetadata generates an ES256 key and a confidential-client
// metadata document (private_key_jwt / jwt-bearer capable) for the
// integration test's own OAuth client identity, served by Caddy at
// testclient.local.habitat.network/client-metadata.json and registered with
// pear via HABITAT_BUILTIN_APP.
func buildTestClientMetadata(t *testing.T, clientID string) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	meta := &pdsclient.ClientMetadata{
		ClientName:      "Habitat Integration Test Client",
		ClientId:        clientID,
		ClientUri:       "https://testclient.local.habitat.network",
		ApplicationType: "web",
		GrantTypes: []string{
			"authorization_code",
			"refresh_token",
			"urn:ietf:params:oauth:grant-type:jwt-bearer",
		},
		Scope:                   "atproto",
		ResponseTypes:           []string{"code"},
		RedirectUris:            []string{"https://testclient.local.habitat.network/oauth-callback"},
		TokenEndpointAuthMethod: "private_key_jwt",
		TokenEndpointAuthSigner: "ES256",
		DpopBoundAccessTokens:   true,
		Jwks: &jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
			Key:       key.Public(),
			KeyID:     "test-client",
			Algorithm: string(jose.ES256),
			Use:       "sig",
		}}},
	}
	metaJSON, err := json.Marshal(meta)
	require.NoError(t, err)
	return key, metaJSON
}

// mustStart starts a container from req, registering cleanup to terminate it
// and dump its logs (in that order, so logs are captured before removal) at
// the end of the test.
func mustStart(ctx context.Context, t *testing.T, req testcontainers.ContainerRequest) testcontainers.Container {
	t.Helper()
	c, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req, Started: true,
	})
	require.NoError(t, err)
	t.Cleanup(func() { dumpLogs(t, c) })
	t.Cleanup(func() { _ = c.Terminate(context.Background()) })
	return c
}

// dumpLogs logs up to 1MB of a container's combined output, for diagnosing
// failures once the stack is torn down.
func dumpLogs(t *testing.T, c testcontainers.Container) {
	rc, err := c.Logs(context.Background())
	if err != nil {
		return
	}
	defer func() { _ = rc.Close() }()
	buf := make([]byte, 1<<20)
	n, _ := rc.Read(buf)
	if n > 0 {
		name, _ := c.Name(context.Background())
		t.Logf("=== logs %s ===\n%s", name, string(buf[:n]))
	}
}

// buildImage builds an image from a repo Dockerfile with the repo root as the
// build context. The container created to trigger the build is never
// started; KeepImage keeps the built image around under tag after the
// (unstarted) container is torn down.
//
// testcontainers composes the final image reference as "Repo:Tag" (see
// (*ContainerRequest).pullImage), so a tag like "pear:itest" must be split
// into Repo="pear" and Tag="itest" - passing it whole as Repo produces the
// invalid reference "pear:itest:<uuid>" (Tag defaults to a random UUID).
func buildImage(ctx context.Context, t *testing.T, dockerfile, tag string) string {
	t.Helper()
	repo, imgTag, ok := strings.Cut(tag, ":")
	require.True(t, ok, "tag must be of the form repo:tag, got %q", tag)
	req := testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			FromDockerfile: testcontainers.FromDockerfile{
				Context:    "..",
				Dockerfile: dockerfile,
				Repo:       repo,
				Tag:        imgTag,
				KeepImage:  true,
			},
		},
	}
	c, err := testcontainers.GenericContainer(ctx, req)
	require.NoError(t, err)
	require.NoError(t, c.Terminate(ctx))
	return tag
}

// startPostgres starts a postgres container and creates the separate "sap"
// database sap needs alongside pear's default "habitat" database. It returns
// the in-network connection strings for pear (habitat db) and sap (sap db).
func startPostgres(ctx context.Context, t *testing.T, netName string) (habitatURL, sapURL string) {
	t.Helper()
	pg := mustStart(ctx, t, testcontainers.ContainerRequest{
		Image:          "postgres:16-alpine",
		Networks:       []string{netName},
		NetworkAliases: map[string][]string{netName: {"postgres"}},
		Env: map[string]string{
			"POSTGRES_USER":     "habitat",
			"POSTGRES_PASSWORD": "habitat",
			"POSTGRES_DB":       "habitat",
		},
		WaitingFor: wait.ForLog("database system is ready to accept connections").
			WithOccurrence(2).
			WithStartupTimeout(60 * time.Second),
	})
	// Create the sap database.
	exitCode, out, err := pg.Exec(ctx, []string{"psql", "-U", "habitat", "-d", "habitat", "-c", "CREATE DATABASE sap"})
	require.NoError(t, err)
	if exitCode != 0 {
		buf := make([]byte, 4096)
		n, _ := out.Read(buf)
		t.Fatalf("CREATE DATABASE sap exited %d: %s", exitCode, string(buf[:n]))
	}
	return "postgres://habitat:habitat@postgres:5432/habitat?sslmode=disable",
		"postgres://habitat:habitat@postgres:5432/sap?sslmode=disable"
}

// pearEnv builds pear's container environment. Secrets are generated fresh
// per test run since nothing outside this stack needs to reproduce them.
func pearEnv(t *testing.T, pgURL, sapClientID, testClientID string) map[string]string {
	t.Helper()
	return map[string]string{
		"HABITAT_DOMAIN":               "pear.local.habitat.network",
		"HABITAT_PORT":                 "8000",
		"HABITAT_DB":                   pgURL,
		"HABITAT_PGURL":                pgURL,
		"HABITAT_PDS_CRED_ENCRYPT_KEY": genKey32(t),
		"HABITAT_OAUTH_SERVER_SECRET":  genKey32(t),
		"HABITAT_OAUTH_CLIENT_SECRET":  genKey32(t),
		"HABITAT_SPACE_SIGNING_KEY":    genP256Multibase(t),
		"HABITAT_BUILTIN_APP":          sapClientID + "," + testClientID,
		"SSL_CERT_FILE":                "/certs/ca.pem",
	}
}

// sapEnv builds sap's container environment. SAP_SECRET must be a
// multibase-encoded atproto private key (sap's OAuth client secret, parsed
// via atcrypto.ParsePrivateMultibase) - the flag's bare-string default
// ("secret") does not parse, so this must always be set explicitly.
func sapEnv(t *testing.T, pgURL string) map[string]string {
	t.Helper()
	return map[string]string{
		"SAP_DOMAIN":        "sap.local.habitat.network",
		"SAP_PORT":          "2580",
		"SAP_INTERNAL_PORT": "2581",
		"SAP_DB":            pgURL,
		"SAP_LOG_LEVEL":     "debug",
		"SAP_SECRET":        genP256Multibase(t),
		"SSL_CERT_FILE":     "/certs/ca.pem",
	}
}

// genKey32 generates a base64-encoded 32-byte key, as consumed by pear's
// encryption/secret flags.
func genKey32(t *testing.T) string {
	t.Helper()
	key, err := encrypt.GenerateKey()
	require.NoError(t, err)
	return key
}

// genP256Multibase generates a multibase-encoded P-256 private key, as
// consumed by pear's space-signing-key flag and sap's secret flag.
func genP256Multibase(t *testing.T) string {
	t.Helper()
	key, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)
	return key.Multibase()
}
