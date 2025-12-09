package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/mdelapenya/tlscert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestEnvironment holds the containers and shared resources for integration tests
type TestEnvironment struct {
	Containers map[string]testcontainers.Container
	CertDir    string
	Network    *testcontainers.DockerNetwork
	ctx        context.Context
	t          *testing.T
}

// NamedContainerRequest pairs a container request with a name for identification
type NamedContainerRequest struct {
	Name    string
	Request testcontainers.GenericContainerRequest
}

// ContainerRequestsFunc is a function that builds container requests given network name and cert directory
type ContainerRequestsFunc func(networkName, certDir string) []NamedContainerRequest

// NewTestEnvironment creates a new test environment with container requests built from the provided function
// All containers will share the same network and certificate directory
func NewTestEnvironment(
	ctx context.Context,
	t *testing.T,
	buildRequests ContainerRequestsFunc,
) *TestEnvironment {
	t.Helper()

	// Use testing.TempDir() for automatic cleanup
	certDir := t.TempDir()

	// Generate self-signed certificates
	generateSelfSignedCert(t, certDir)

	// Create a shared Docker network for container-to-container communication
	testNetwork, err := network.New(ctx,
		network.WithCheckDuplicate(),
		network.WithDriver("bridge"),
	)
	require.NoError(t, err, "failed to create network")
	t.Cleanup(func() {
		if err := testNetwork.Remove(ctx); err != nil {
			t.Logf("Failed to remove network: %v", err)
		}
	})

	// Build named container requests using the network and cert directory
	namedRequests := buildRequests(testNetwork.Name, certDir)

	// Extract requests for parallel startup
	requests := make(testcontainers.ParallelContainerRequest, len(namedRequests))
	for i, nr := range namedRequests {
		requests[i] = nr.Request
	}

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

	// Map containers by their logical name (from namedRequests)
	// We need to inspect each container to find its actual name and match it back
	containerMap := make(map[string]testcontainers.Container)

	// Create a mapping from container name prefix to logical name
	nameMapping := make(map[string]string)
	for _, nr := range namedRequests {
		// Container names have a prefix like "integration-pds"
		nameMapping[nr.Request.ContainerRequest.Name] = nr.Name
	}

	for _, container := range allContainers {
		// Inspect the container to get its name
		inspect, err := container.Inspect(ctx)
		require.NoError(t, err, "failed to inspect container")

		// Container name from inspect starts with "/"
		actualName := inspect.Name
		if len(actualName) > 0 && actualName[0] == '/' {
			actualName = actualName[1:]
		}

		// Find the logical name by finding the longest matching container name
		// This handles cases like "integration-pds-proxy" vs "integration-pds"
		var logicalName string
		var longestMatch string
		for containerName, name := range nameMapping {
			// Check if actual name starts with container name
			if len(actualName) >= len(containerName) &&
				actualName[:len(containerName)] == containerName {
				// If this is a longer match than what we found before, use it
				if len(containerName) > len(longestMatch) {
					longestMatch = containerName
					logicalName = name
				}
			}
		}

		if logicalName == "" {
			t.Logf("Warning: Could not map container %s to a logical name", actualName)
			continue
		}

		containerMap[logicalName] = container

		// Register cleanup
		c := container
		n := logicalName
		t.Cleanup(func() {
			if err := c.Terminate(ctx); err != nil {
				t.Logf("Failed to terminate container %s: %v", n, err)
			}
		})

		// Stream container logs to test output
		go func(containerName string, cont testcontainers.Container) {
			logs, err := cont.Logs(ctx)
			if err != nil {
				t.Logf("Failed to get logs for container %s: %v", containerName, err)
				return
			}
			defer logs.Close()

			buf := new(bytes.Buffer)
			_, err = buf.ReadFrom(logs)
			if err != nil {
				t.Logf("Failed to read logs for container %s: %v", containerName, err)
				return
			}

			t.Logf("Container %s logs:\n%s", containerName, buf.String())
		}(logicalName, container)
	}

	return &TestEnvironment{
		Containers: containerMap,
		CertDir:    certDir,
		Network:    testNetwork,
		ctx:        ctx,
		t:          t,
	}
}

// LogContainerLogs logs a container's logs using t.Logf
func (e *TestEnvironment) LogContainerLogs(name string) {
	e.t.Helper()

	container, ok := e.Containers[name]
	if !ok {
		e.t.Logf("Container %s not found", name)
		return
	}

	logs, err := container.Logs(e.ctx)
	if err != nil {
		e.t.Logf("Failed to get %s logs: %v", name, err)
		return
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(logs)
	if err != nil {
		e.t.Logf("Failed to read %s logs: %v", name, err)
		return
	}

	e.t.Logf("%s Container Logs:\n%s", name, buf.String())
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

// createNginxConfig creates the nginx configuration file for the PDS proxy
func createNginxConfig(t *testing.T, certDir string) string {
	t.Helper()

	nginxConfig := `
events {
    worker_connections 1024;
}

http {
    # Use Docker's internal DNS resolver
    resolver 127.0.0.11 valid=10s;
    resolver_timeout 5s;

    server {
        listen 443 ssl;
        server_name pds.example.com;

        ssl_certificate /certs/fullchain.pem;
        ssl_certificate_key /certs/privkey.pem;

        location / {
            # Use variable to force DNS resolution at request time
            set $upstream http://pds-backend:3000;
            proxy_pass $upstream;
            proxy_set_header Host pds.example.com;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto https;
        }
    }
}
`

	nginxConfigPath := filepath.Join(certDir, "nginx.conf")
	err := os.WriteFile(nginxConfigPath, []byte(nginxConfig), 0o644)
	require.NoError(t, err, "failed to write nginx config")

	return nginxConfigPath
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
	networkName, certDir, nginxConfigPath string,
) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:           "integration-pds-proxy",
			Image:          "nginx:alpine",
			ExposedPorts:   []string{"443/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {"pds.example.com"}},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.Mounts = append(hc.Mounts,
					mount.Mount{
						Type:   mount.TypeBind,
						Source: certDir,
						Target: "/certs",
					},
					mount.Mount{
						Type:     mount.TypeBind,
						Source:   nginxConfigPath,
						Target:   "/etc/nginx/nginx.conf",
						ReadOnly: true,
					},
				)
			},
			WaitingFor:      wait.ForListeningPort("443/tcp"),
			HostAccessPorts: []int{443},
		},
		Started: true,
	}
}

// PriviContainerRequest creates a container request for the Privi service with HTTPS enabled
func PriviContainerRequest(networkName, certDir string) testcontainers.GenericContainerRequest {
	return testcontainers.GenericContainerRequest{
		ContainerRequest: testcontainers.ContainerRequest{
			Name:  "integration-privi",
			Image: "privi:latest",
			Env: map[string]string{
				"HABITAT_DB":         "/tmp/repo.db",
				"HABITAT_KEYFILE":    "/tmp/key.jwk",
				"HABITAT_DOMAIN":     "privi.habitat",
				"HABITAT_PORT":       "443",
				"HABITAT_HTTPSCERTS": "/certs/",
				"SSL_CERT_FILE":      "/certs/fullchain.pem",
			},
			ExposedPorts:   []string{"443/tcp"},
			Networks:       []string{networkName},
			NetworkAliases: map[string][]string{networkName: {"privi.habitat"}},
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.Mounts = append(hc.Mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: certDir,
					Target: "/certs",
				})
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

// StandardIntegrationRequests creates the standard set of named container requests for integration tests
func StandardIntegrationRequests(
	t *testing.T,
	networkName, certDir string,
) []NamedContainerRequest {
	t.Helper()

	nginxConfigPath := createNginxConfig(t, certDir)

	return []NamedContainerRequest{
		{Name: "pds", Request: PDSContainerRequest(networkName)},
		{
			Name:    "pds-proxy",
			Request: PDSProxyContainerRequest(networkName, certDir, nginxConfigPath),
		},
		{Name: "privi", Request: PriviContainerRequest(networkName, certDir)},
		{Name: "selenium", Request: SeleniumContainerRequest(networkName)},
		{Name: "frontend", Request: FrontendContainerRequest(networkName, certDir)},
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
			HostConfigModifier: func(hc *container.HostConfig) {
				hc.Mounts = append(hc.Mounts, mount.Mount{
					Type:   mount.TypeBind,
					Source: certDir,
					Target: "/certs",
				})
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
