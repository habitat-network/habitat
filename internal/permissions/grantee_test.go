package permissions

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGranteesFromInterface(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		result, err := ParseGranteesFromInterface([]interface{}{})
		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("valid did grantee", func(t *testing.T) {
		result, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#didGrantee",
				"did":   "did:plc:abc123",
			},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, DIDGrantee("did:plc:abc123"), result[0])
	})

	t.Run("valid clique grantee", func(t *testing.T) {
		result, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#cliqueRef",
				"uri":   "habitat://did:plc:abc123/network.habitat.clique/my-clique",
			},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, CliqueGrantee("habitat://did:plc:abc123/network.habitat.clique/my-clique"), result[0])
	})

	t.Run("multiple grantees", func(t *testing.T) {
		result, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#didGrantee",
				"did":   "did:plc:alice",
			},
			map[string]interface{}{
				"$type": "network.habitat.grantee#didGrantee",
				"did":   "did:plc:bob",
			},
			map[string]interface{}{
				"$type": "network.habitat.grantee#cliqueRef",
				"uri":   "habitat://did:plc:alice/network.habitat.clique/team",
			},
		})
		require.NoError(t, err)
		require.Len(t, result, 3)
		require.Equal(t, DIDGrantee("did:plc:alice"), result[0])
		require.Equal(t, DIDGrantee("did:plc:bob"), result[1])
		require.Equal(t, CliqueGrantee("habitat://did:plc:alice/network.habitat.clique/team"), result[2])
	})

	t.Run("missing $type field", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"did": "did:plc:abc123",
			},
		})
		require.Error(t, err)
	})

	t.Run("unknown $type", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#unknown",
				"did":   "did:plc:abc123",
			},
		})
		require.Error(t, err)
	})

	t.Run("did grantee missing did field", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#didGrantee",
			},
		})
		require.Error(t, err)
	})

	t.Run("clique grantee missing uri field", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#cliqueRef",
			},
		})
		require.Error(t, err)
	})

	t.Run("non-map element", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{"not-a-map"})
		require.Error(t, err)
	})

	t.Run("clique grantee with invalid uri", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#cliqueRef",
				"uri":   "habitat://did:plc:abc123/network.habitat.not.a.clique/rkey",
			},
		})
		require.Error(t, err)
	})
}

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
