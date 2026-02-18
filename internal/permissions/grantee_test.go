package permissions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHabitatClique(t *testing.T) {
	t.Run("valid clique", func(t *testing.T) {
		uri, err := parseHabitatClique("habitat://did:plc:abc123/network.habitat.clique/clique-rkey")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", uri.Authority().String())
		require.Equal(t, "network.habitat.clique", uri.Collection().String())
		require.Equal(t, "clique-rkey", string(uri.RecordKey()))
		require.Equal(t, "network.habitat.clique/clique-rkey", uri.Path())
		require.Equal(t, uri, uri.Normalize())
	})

	t.Run("invalid clique", func(t *testing.T) {
		_, err := parseHabitatClique("habitat://did:plc:abc123/network.habitat.not.a.clique/clique-rkey")
		require.Error(t, err)
	})
}
