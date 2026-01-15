package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mdelapenya/tlscert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestEnvironment holds the containers and shared resources for integration tests
type TestEnvironment struct {
	Containers        []testcontainers.Container
	PostgresContainer *postgres.PostgresContainer
	ctx               context.Context
	t                 *testing.T
}

// Get returns the container with the given logical name, or nil if not found
func (e *TestEnvironment) Get(name string) testcontainers.Container {
	// Container names follow the pattern: /integration-{name}
	expectedName := "/integration-" + name

	for _, container := range e.Containers {
		inspect, err := container.Inspect(e.ctx)
		require.NoError(e.t, err, "failed to inspect container")

		if inspect.Name == expectedName {
			return container
		}
	}
	return nil
}

// NewTestEnvironment creates a new test environment with containers built from the provided function
func NewTestEnvironment(
	ctx context.Context,
	t *testing.T,
	requests testcontainers.ParallelContainerRequest,
	postgresContainer *postgres.PostgresContainer,
) *TestEnvironment {
	t.Helper()

	// Start all containers in parallel
	allContainers, err := testcontainers.ParallelContainers(
		ctx,
		requests,
		testcontainers.ParallelContainersOptions{},
	)
	if err != nil {
		var pcErr testcontainers.ParallelContainersError
		if errors, ok := err.(testcontainers.ParallelContainersError); ok {
			pcErr = errors
			for _, reqErr := range pcErr.Errors {
				t.Logf("Container request failed: %v", reqErr.Error)
			}
		}
		require.NoError(t, err, "failed to start containers in parallel")
	}
	t.Cleanup(func() {
		for _, container := range allContainers {
			if err := container.Terminate(ctx); err != nil {
				t.Logf("Failed to terminate container: %v", err)
			}
		}
	})
	t.Cleanup(func() {
		for _, container := range allContainers {
			inspect, err := container.Inspect(ctx)
			if err != nil {
				t.Logf("Failed to inspect container: %v", err)
				continue
			}
			logs, err := container.Logs(ctx)
			if err != nil {
				t.Logf("Failed to get logs for container %s: %v", inspect.Name, err)
				continue
			}
			defer logs.Close()
			buf := new(bytes.Buffer)
			_, err = buf.ReadFrom(logs)
			if err != nil {
				t.Logf("Failed to read logs for container %s: %v", inspect.Name, err)
			} else {
				t.Logf("=============== Container %s logs: ================\n", inspect.Name)
				t.Log(buf.String() + "\n")
			}

		}
	})
	return &TestEnvironment{
		Containers:        allContainers,
		PostgresContainer: postgresContainer,
		ctx:               ctx,
		t:                 t,
	}
}

// generateSelfSignedCert creates a self-signed certificate for HTTPS testing using tlscert
func generateSelfSignedCert(t *testing.T, certDir string) {
	t.Helper()

	// Generate self-signed certificate using tlscert
	// Include privi.habitat, pds.example.com, and frontend.habitat for all containers to use
	cert := tlscert.SelfSignedFromRequest(tlscert.Request{
		Name:      "integration-test",
		Host:      "localhost,127.0.0.1,privi.habitat,pds.example.com,frontend.habitat",
		ParentDir: certDir,
	})

	require.NotNil(t, cert, "failed to generate certificate")

	// Rename the generated files to match the expected names
	newCertPath := filepath.Join(certDir, "fullchain.pem")
	newKeyPath := filepath.Join(certDir, "privkey.pem")

	err := os.Rename(cert.CertPath, newCertPath)
	require.NoError(t, err, "failed to rename cert file")

	err = os.Rename(cert.KeyPath, newKeyPath)
	require.NoError(t, err, "failed to rename key file")
}

// PDSContainerRequest creates a container request for the Bluesky PDS (HTTP only, behind reverse proxy)
func PDSContainerRequest(networkName string) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:  "integration-pds",
			Image: "ghcr.io/bluesky-social/pds:latest",
			Env: map[string]string{
				"PDS_HOSTNAME":                "pds.example.com",
				"PDS_PORT":                    "3000",
				"PDS_SERVICE_DID":             "did:web:pds.example.com",
				"PDS_ADMIN_PASSWORD":          "password",
				"PDS_JWT_SECRET":              "bd6df801372d7058e1ce472305d7fc2e",
				"PDS_DATA_DIRECTORY":          "/pds",
				"PDS_BLOBSTORE_DISK_LOCATION": "/pds/blocks",
				"PDS_DID_PLC_URL":             "https://plc.directory",
				"PDS_DEV_MODE":                "true",
				"PDS_PLC_ROTATION_KEY_K256_PRIVATE_KEY_HEX": "5290bb1866a03fb23b09a6ffd64d21f6a4ebf624eaa301930eeb81740699239c",
				"PDS_INVITE_REQUIRED":                       "false",
				"DEBUG":                                     "1",
				"LOG_LEVEL":                                 "debug",
				"LOG_ENABLED":                               "1",
				"NODE_TLS_REJECT_UNAUTHORIZED":              "0",
			},
			Tmpfs: map[string]string{
				"/pds": "rw,size=100m",
			},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {"pds-backend"}},
			WaitingFor:     wait.ForListeningPort("3000/tcp"),
		},
		Started: true,
	}
}

// PDSProxyContainerRequest creates a container request for nginx reverse proxy with TLS termination
func PDSProxyContainerRequest(
	networkName, certDir string,
) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:           "integration-pds-proxy",
			Image:          "nginx:alpine",
			ExposedPorts:   []string{"443/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {"pds.example.com"}},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      filepath.Join(certDir, "fullchain.pem"),
					ContainerFilePath: "/certs/fullchain.pem",
					FileMode:          0o644,
				},
				{
					HostFilePath:      filepath.Join(certDir, "privkey.pem"),
					ContainerFilePath: "/certs/privkey.pem",
					FileMode:          0o644,
				},
				{
					HostFilePath:      "pds-proxy-nginx.conf",
					ContainerFilePath: "/etc/nginx/nginx.conf",
					FileMode:          0o644,
				},
			},
			WaitingFor:      wait.ForListeningPort("443/tcp"),
			HostAccessPorts: []int{443},
		},
		Started: true,
	}
}

// PriviContainerRequest creates a container request for the Privi service with HTTPS enabled
func PriviContainerRequest(
	networkName, certDir, pgURL string,
) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:  "integration-privi",
			Image: "privi:latest",
			Env: map[string]string{
				"HABITAT_KEYFILE":              "/tmp/key.jwk",
				"HABITAT_DOMAIN":               "privi.habitat",
				"HABITAT_PORT":                 "443",
				"HABITAT_HTTPSCERTS":           "/certs/",
				"SSL_CERT_FILE":                "/certs/fullchain.pem",
				"HABITAT_PDS_CRED_ENCRYPT_KEY": "GB2ZuB3tRBNGK8KNyCln+pkEylqxutrAI09xfY8njfI=",
				"HABITAT_PGURL":                pgURL,
			},
			ExposedPorts:   []string{"443/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {"privi.habitat"}},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      filepath.Join(certDir, "fullchain.pem"),
					ContainerFilePath: "/certs/fullchain.pem",
					FileMode:          0o644,
				},
				{
					HostFilePath:      filepath.Join(certDir, "privkey.pem"),
					ContainerFilePath: "/certs/privkey.pem",
					FileMode:          0o644,
				},
			},
			WaitingFor: wait.ForHTTP("/.well-known/did.json").
				WithPort("443/tcp").
				WithTLS(true, &tls.Config{InsecureSkipVerify: true}).
				WithStartupTimeout(30 * time.Second),
		},
		Started: true,
	}
}

// SeleniumContainerRequest creates a container request for Selenium Standalone Chrome
func SeleniumContainerRequest(networkName string) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:         "integration-selenium",
			Image:        "seleniarm/standalone-chromium:latest",
			ExposedPorts: []string{"4444/tcp"},
			Networks:     []string{networkName},
			Env: map[string]string{
				"SE_NODE_MAX_SESSIONS":    "5",
				"SE_NODE_SESSION_TIMEOUT": "300",
			},
			WaitingFor: wait.ForHTTP("/wd/hub/status").
				WithPort("4444/tcp").
				WithStartupTimeout(60 * time.Second),
		},
		Started: true,
	}
}

// NewPostgresContainer creates and starts a PostgreSQL container using the postgres module
func NewPostgresContainer(
	ctx context.Context,
	t *testing.T,
	networkName string,
) (*postgres.PostgresContainer, string) {
	t.Helper()

	postgresContainer, err := postgres.Run(ctx,
		"postgres:16-alpine",
		postgres.WithDatabase("habitat"),
		postgres.WithUsername("habitat"),
		postgres.WithPassword("habitat"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").WithOccurrence(2),
		),
		testcontainers.CustomizeRequest(testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Name:           "integration-postgres",
				Networks:       []string{networkName},
				NetworkAliases: map[string][]string{networkName: {"postgres"}},
			},
		}),
	)
	require.NoError(t, err, "failed to start postgres container")

	t.Cleanup(func() {
		if err := postgresContainer.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate postgres container: %v", err)
		}
	})

	// Return the container and the internal Docker network connection string
	pgURL := "postgres://habitat:habitat@postgres:5432/habitat?sslmode=disable"
	return postgresContainer, pgURL
}

// StandardIntegrationRequests creates the standard set of named container requests for integration tests
func StandardIntegrationRequests(
	t *testing.T,
	networkName, certDir, pgURL string,
) testcontainers.ParallelContainerRequest {
	t.Helper()

	return []testcontainers.GenericContainerRequest{
		PDSContainerRequest(networkName),
		PDSProxyContainerRequest(networkName, certDir),
		PriviContainerRequest(networkName, certDir, pgURL),
		SeleniumContainerRequest(networkName),
		FrontendContainerRequest(networkName, certDir),
	}
}

func FrontendContainerRequest(networkName, certDir string) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:           "integration-frontend",
			Image:          "frontend:latest",
			ExposedPorts:   []string{"443/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {"frontend.habitat"}},
			Files: []testcontainers.ContainerFile{
				{
					HostFilePath:      filepath.Join(certDir, "fullchain.pem"),
					ContainerFilePath: "/certs/fullchain.pem",
					FileMode:          0o644,
				},
				{
					HostFilePath:      filepath.Join(certDir, "privkey.pem"),
					ContainerFilePath: "/certs/privkey.pem",
					FileMode:          0o644,
				},
			},
			WaitingFor: wait.ForHTTP("/").
				WithPort("443/tcp").
				WithTLS(true, &tls.Config{InsecureSkipVerify: true}).
				WithStartupTimeout(30 * time.Second),
			HostAccessPorts: []int{443},
		},
		Started: true,
	}
}
