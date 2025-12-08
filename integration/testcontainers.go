package integration

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/mdelapenya/tlscert"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

func init() {
	// Configure Docker host for Colima if DOCKER_HOST is not already set
	if os.Getenv("DOCKER_HOST") == "" {
		// Check if Colima socket exists
		colimaSocket := filepath.Join(os.Getenv("HOME"), ".colima/default/docker.sock")
		if _, err := os.Stat(colimaSocket); err == nil {
			os.Setenv("DOCKER_HOST", "unix://"+colimaSocket)
		}
	}
}

// TestEnvironment holds all the containers and configuration for integration tests
type TestEnvironment struct {
	PriviContainer    testcontainers.Container
	PDSContainer      testcontainers.Container
	PDSProxyContainer testcontainers.Container
	SeleniumContainer testcontainers.Container
	PriviURL          string
	PDSURL            string
	SeleniumURL       string
	CertDir           string
	network           *testcontainers.DockerNetwork
	ctx               context.Context
}

// NewTestEnvironment creates a new test environment with all required containers
func NewTestEnvironment(ctx context.Context, t *testing.T) (*TestEnvironment, error) {
	env := &TestEnvironment{
		ctx: ctx,
	}

	// Use testing.TempDir() for automatic cleanup
	// Since we're passing cert contents as env vars, Docker doesn't need to mount this
	certDir := t.TempDir()
	env.CertDir = certDir

	// Generate self-signed certificates
	if err := generateSelfSignedCert(certDir); err != nil {
		return nil, fmt.Errorf("failed to generate certificates: %w", err)
	}

	// Create a shared Docker network for container-to-container communication
	testNetwork, err := network.New(ctx,
		network.WithCheckDuplicate(),
		network.WithDriver("bridge"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create network: %w", err)
	}
	env.network = testNetwork

	// Start PDS container (HTTP only, no network alias)
	pdsContainer, err := startPDSContainer(ctx, testNetwork)
	if err != nil {
		env.Cleanup()
		return nil, fmt.Errorf("failed to start pds container: %w", err)
	}
	env.PDSContainer = pdsContainer

	// Start nginx reverse proxy for PDS with TLS termination
	pdsProxyContainer, pdsURL, err := startPDSProxyContainer(ctx, certDir, testNetwork)
	if err != nil {
		env.Cleanup()
		return nil, fmt.Errorf("failed to start pds proxy container: %w", err)
	}
	env.PDSProxyContainer = pdsProxyContainer
	env.PDSURL = pdsURL

	// Start Privi container with HTTPS on the same network
	priviContainer, priviURL, priviNetworkURL, err := startPriviContainer(ctx, certDir, testNetwork)
	if err != nil {
		env.Cleanup()
		return nil, fmt.Errorf("failed to start privi container: %w", err)
	}
	env.PriviContainer = priviContainer
	env.PriviURL = priviURL

	// Start Selenium container on the same network
	seleniumContainer, seleniumURL, err := startSeleniumContainer(ctx, testNetwork)
	if err != nil {
		env.Cleanup()
		return nil, fmt.Errorf("failed to start selenium container: %w", err)
	}
	env.SeleniumContainer = seleniumContainer
	env.SeleniumURL = seleniumURL

	t.Logf("Privi URL (host): %s", priviURL)
	t.Logf("Privi URL (Docker network): %s", priviNetworkURL)
	t.Logf("Selenium URL: %s", seleniumURL)

	return env, nil
}

// Cleanup stops all containers and cleans up temporary files
func (e *TestEnvironment) Cleanup() {
	if e.SeleniumContainer != nil {
		_ = e.SeleniumContainer.Terminate(e.ctx)
	}
	if e.PriviContainer != nil {
		_ = e.PriviContainer.Terminate(e.ctx)
	}
	if e.PDSProxyContainer != nil {
		_ = e.PDSProxyContainer.Terminate(e.ctx)
	}
	if e.PDSContainer != nil {
		_ = e.PDSContainer.Terminate(e.ctx)
	}
	if e.network != nil {
		_ = e.network.Remove(e.ctx)
	}
	if e.CertDir != "" {
		_ = os.RemoveAll(e.CertDir)
	}
}

// GetPDSLogs returns the logs from the PDS container
func (e *TestEnvironment) GetPDSLogs() (string, error) {
	if e.PDSContainer == nil {
		return "", fmt.Errorf("PDS container not initialized")
	}

	logs, err := e.PDSContainer.Logs(e.ctx)
	if err != nil {
		return "", err
	}
	defer logs.Close()

	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(logs)
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (e *TestEnvironment) GetPDSContainerID() (string, error) {
	if e.PDSContainer == nil {
		return "", fmt.Errorf("PDS container not initialized")
	}
	return e.PDSContainer.GetContainerID(), nil
}

// generateSelfSignedCert creates a self-signed certificate for HTTPS testing using tlscert
func generateSelfSignedCert(certDir string) error {
	// Generate self-signed certificate using tlscert
	// Include both privi.habitat and pds.example.com for both containers to use
	cert := tlscert.SelfSignedFromRequest(tlscert.Request{
		Name:      "integration-test",
		Host:      "localhost,127.0.0.1,privi.habitat,pds.example.com",
		ParentDir: certDir,
	})

	if cert == nil {
		return fmt.Errorf("failed to generate certificate")
	}

	// Rename the generated files to match the expected names
	newCertPath := filepath.Join(certDir, "fullchain.pem")
	newKeyPath := filepath.Join(certDir, "privkey.pem")

	if err := os.Rename(cert.CertPath, newCertPath); err != nil {
		return fmt.Errorf("failed to rename cert file: %w", err)
	}

	if err := os.Rename(cert.KeyPath, newKeyPath); err != nil {
		return fmt.Errorf("failed to rename key file: %w", err)
	}

	return nil
}

// startPriviContainer starts the Privi service container with HTTPS enabled
func startPriviContainer(
	ctx context.Context,
	certDir string,
	testNetwork *testcontainers.DockerNetwork,
) (testcontainers.Container, string, string, error) {
	req := testcontainers.ContainerRequest{
		Image: "privi:latest",
		Env: map[string]string{
			"HABITAT_DB":         "/tmp/repo.db",
			"HABITAT_KEYFILE":    "/tmp/key.jwk",
			"HABITAT_DOMAIN":     "privi.habitat",
			"HABITAT_PORT":       "443",
			"HABITAT_HTTPSCERTS": "/certs/",
			"SSL_CERT_FILE":      "/certs/fullchain.pem", // Trust self-signed certs for HTTPS requests
		},
		ExposedPorts:   []string{"443/tcp"},
		Networks:       []string{testNetwork.Name},
		NetworkAliases: map[string][]string{testNetwork.Name: {"privi.habitat"}},
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
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", "", err
	}

	port, err := container.MappedPort(ctx, "443")
	if err != nil {
		return nil, "", "", err
	}

	hostURL := fmt.Sprintf("https://%s:%s", host, port.Port())
	networkURL := "https://privi.habitat"
	return container, hostURL, networkURL, nil
}

// startPDSContainer starts the Bluesky PDS container (HTTP only, behind reverse proxy)
func startPDSContainer(
	ctx context.Context,
	testNetwork *testcontainers.DockerNetwork,
) (testcontainers.Container, error) {
	req := testcontainers.ContainerRequest{
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
			"NODE_TLS_REJECT_UNAUTHORIZED":              "0", // Accept self-signed certs in dev mode
		},
		Tmpfs: map[string]string{
			"/pds": "rw,size=100m",
		},
		Networks:       []string{testNetwork.Name},
		NetworkAliases: map[string][]string{testNetwork.Name: {"pds-backend"}},
		WaitingFor:     wait.ForListeningPort("3000/tcp"),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, err
	}

	return container, nil
}

// startPDSProxyContainer starts an nginx reverse proxy for PDS with TLS termination
func startPDSProxyContainer(
	ctx context.Context,
	certDir string,
	testNetwork *testcontainers.DockerNetwork,
) (testcontainers.Container, string, error) {
	// Create nginx config for reverse proxy
	nginxConfig := `
events {
    worker_connections 1024;
}

http {
    server {
        listen 443 ssl;
        server_name pds.example.com;

        ssl_certificate /certs/fullchain.pem;
        ssl_certificate_key /certs/privkey.pem;

        location / {
            proxy_pass http://pds-backend:3000;
            proxy_set_header Host pds.example.com;
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto https;
        }
    }
}
`

	// Write nginx config to cert directory
	nginxConfigPath := filepath.Join(certDir, "nginx.conf")
	if err := os.WriteFile(nginxConfigPath, []byte(nginxConfig), 0644); err != nil {
		return nil, "", fmt.Errorf("failed to write nginx config: %w", err)
	}

	req := testcontainers.ContainerRequest{
		Image:          "nginx:alpine",
		ExposedPorts:   []string{"443/tcp"},
		Networks:       []string{testNetwork.Name},
		NetworkAliases: map[string][]string{testNetwork.Name: {"pds.example.com"}},
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
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", err
	}

	port, err := container.MappedPort(ctx, "443")
	if err != nil {
		return nil, "", err
	}

	url := fmt.Sprintf("https://%s:%s", host, port.Port())
	return container, url, nil
}

// startSeleniumContainer starts a Selenium Standalone Chrome container
func startSeleniumContainer(
	ctx context.Context,
	testNetwork *testcontainers.DockerNetwork,
) (testcontainers.Container, string, error) {
	req := testcontainers.ContainerRequest{
		Image:        "seleniarm/standalone-chromium:latest", // ARM64-compatible image
		ExposedPorts: []string{"4444/tcp"},
		Networks:     []string{testNetwork.Name},
		Env: map[string]string{
			"SE_NODE_MAX_SESSIONS":    "5",
			"SE_NODE_SESSION_TIMEOUT": "300",
		},
		WaitingFor: wait.ForHTTP("/wd/hub/status").
			WithPort("4444/tcp").
			WithStartupTimeout(60 * time.Second),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	if err != nil {
		return nil, "", err
	}

	host, err := container.Host(ctx)
	if err != nil {
		return nil, "", err
	}

	port, err := container.MappedPort(ctx, "4444")
	if err != nil {
		return nil, "", err
	}

	url := fmt.Sprintf("http://%s:%s/wd/hub", host, port.Port())
	return container, url, nil
}
