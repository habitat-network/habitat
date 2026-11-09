package llmgateway

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Every provider offered is one pear knows how to call, and vice versa.
func TestProvidersAreAllSpecified(t *testing.T) {
	require.Len(t, providers, len(Providers))
	for _, p := range Providers {
		require.Contains(t, providers, p)
	}
}
