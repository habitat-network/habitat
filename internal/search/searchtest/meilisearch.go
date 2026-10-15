// Package searchtest starts Meilisearch for tests.
package searchtest

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/habitat-network/habitat/internal/search"
)

// URLEnv names an environment variable holding the URL of a running
// Meilisearch, with no master key, for tests to use instead of starting a
// container.
const URLEnv = "HABITAT_TEST_MEILISEARCH_URL"

// image is the Meilisearch version tests run against. Keep it in step with
// `meilisearch` in .prototools and meilisearch_version in
// terraform/meilisearch.
const image = "getmeili/meilisearch:v1.54.2"

var (
	startOnce sync.Once
	sharedURL string
	startErr  error
)

// NewIndex returns a [search.Meilisearch] over a fresh index. Tests share one
// Meilisearch server, from [URLEnv] or a container started on first use, and
// each gets its own index in it.
func NewIndex(t *testing.T, opts ...search.MeilisearchOption) *search.Meilisearch {
	t.Helper()
	return OpenIndex(t, NewUID(t), opts...)
}

// NewUID returns a fresh index name for [OpenIndex].
func NewUID(t *testing.T) string {
	t.Helper()
	suffix := make([]byte, 8)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	return "test_" + hex.EncodeToString(suffix)
}

// OpenIndex returns a [search.Meilisearch] over the index uid on the shared
// server, so a test can open one index again with different options.
func OpenIndex(t *testing.T, uid string, opts ...search.MeilisearchOption) *search.Meilisearch {
	t.Helper()
	idx, err := search.NewMeilisearch(t.Context(), serverURL(t), "", uid, opts...)
	require.NoError(t, err)
	return idx
}

// URL returns the URL of the shared Meilisearch server, with no master key.
func URL(t *testing.T) string {
	t.Helper()
	return serverURL(t)
}

func serverURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv(URLEnv); url != "" {
		return url
	}
	// The container can only reach host ports named when it starts, so the
	// fake embedder starts first.
	embedderPort := startEmbedder(t)
	startOnce.Do(func() {
		// The container outlives the test that started it, since later
		// tests share it; testcontainers' reaper removes it after the run.
		ctx := t.Context()
		var c testcontainers.Container
		c, startErr = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        image,
				ExposedPorts: []string{"7700/tcp"},
				Env: map[string]string{
					"MEILI_NO_ANALYTICS": "true",
					// The fake embedder is on the host, at an internal IP
					// Meilisearch otherwise refuses to call.
					"MEILI_EXPERIMENTAL_ALLOWED_IP_NETWORKS": "any",
				},
				WaitingFor:      wait.ForHTTP("/health").WithPort("7700/tcp"),
				HostAccessPorts: []int{embedderPort},
			},
			Started: true,
		})
		if startErr != nil {
			return
		}
		sharedURL, startErr = c.PortEndpoint(ctx, "7700/tcp", "http")
	})
	require.NoError(t, startErr)
	return sharedURL
}
