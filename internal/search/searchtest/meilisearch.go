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

// image is the Meilisearch version tests run against.
const image = "getmeili/meilisearch:v1.54.0"

var (
	startOnce sync.Once
	sharedURL string
	startErr  error
)

// NewIndex returns a [search.Meilisearch] over a fresh index. Tests share one
// Meilisearch server, from [URLEnv] or a container started on first use, and
// each gets its own index in it.
func NewIndex(t *testing.T) *search.Meilisearch {
	t.Helper()
	url := serverURL(t)
	suffix := make([]byte, 8)
	_, err := rand.Read(suffix)
	require.NoError(t, err)
	idx, err := search.NewMeilisearch(t.Context(), url, "", "test_"+hex.EncodeToString(suffix))
	require.NoError(t, err)
	return idx
}

func serverURL(t *testing.T) string {
	t.Helper()
	if url := os.Getenv(URLEnv); url != "" {
		return url
	}
	startOnce.Do(func() {
		// The container outlives the test that started it, since later
		// tests share it; testcontainers' reaper removes it after the run.
		ctx := t.Context()
		var c testcontainers.Container
		c, startErr = testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
			ContainerRequest: testcontainers.ContainerRequest{
				Image:        image,
				ExposedPorts: []string{"7700/tcp"},
				Env:          map[string]string{"MEILI_NO_ANALYTICS": "true"},
				WaitingFor:   wait.ForHTTP("/health").WithPort("7700/tcp"),
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
