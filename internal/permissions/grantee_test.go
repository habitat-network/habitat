package permissions

import (
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
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
				"$type":  "network.habitat.grantee#clique",
				"clique": "clique:did:plc:abc123/my-clique",
			},
		})
		require.NoError(t, err)
		require.Len(t, result, 1)
		require.Equal(t, habitat_syntax.Clique("clique:did:plc:abc123/my-clique"), result[0])
	})

	t.Run("multiple grantees", func(t *testing.T) {
		input := []interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#didGrantee",
				"did":   "did:plc:alice",
			},
			map[string]interface{}{
				"$type": "network.habitat.grantee#didGrantee",
				"did":   "did:plc:bob",
			},
			map[string]interface{}{
				"$type":  "network.habitat.grantee#clique",
				"clique": "clique:did:plc:alice/team",
			},
		}

		result, err := ParseGranteesFromInterface(input)
		require.NoError(t, err)
		require.Len(t, result, 3)
		require.Equal(t, DIDGrantee("did:plc:alice"), result[0])
		require.Equal(t, DIDGrantee("did:plc:bob"), result[1])
		require.Equal(t, habitat_syntax.Clique("clique:did:plc:alice/team"), result[2])

		constructed := ConstructInterfaceFromGrantees(result)
		require.Equal(t, constructed, input)
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

	t.Run("clique grantee missing clique field", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type": "network.habitat.grantee#clique",
			},
		})
		require.Error(t, err)
	})

	t.Run("non-map element", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{"not-a-map"})
		require.Error(t, err)
	})

	t.Run("clique grantee with invalid clique", func(t *testing.T) {
		_, err := ParseGranteesFromInterface([]interface{}{
			map[string]interface{}{
				"$type":  "network.habitat.grantee#clique",
				"clique": "not-a-valid-clique",
			},
		})
		require.Error(t, err)
	})
}

func TestParseClique(t *testing.T) {
	t.Run("valid clique", func(t *testing.T) {
		clique, err := habitat_syntax.ParseClique("clique:did:plc:abc123/clique-rkey")
		require.NoError(t, err)
		require.Equal(t, syntax.DID("did:plc:abc123"), clique.Authority())
		require.Equal(t, "clique-rkey", clique.Key())
	})

	t.Run("invalid clique", func(t *testing.T) {
		_, err := habitat_syntax.ParseClique("not-a-valid-clique-format")
		require.Error(t, err)
	})
}
