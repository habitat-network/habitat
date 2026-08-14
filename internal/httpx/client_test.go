package httpx

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultClient(t *testing.T) {
	client := NewClient()
	require.NotNil(t, client)
}
