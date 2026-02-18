package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstructHabitatUri(t *testing.T) {
	uri := ConstructHabitatUri("did:plc:abc123", "network.habitat.post", "rkey-1")
	require.Equal(t, HabitatURI("habitat://did:plc:abc123/network.habitat.post/rkey-1"), uri)

	// Verify the parts round-trip correctly
	did, collection, rkey, err := uri.ExtractParts()
	require.NoError(t, err)
	require.Equal(t, "did:plc:abc123", did.String())
	require.Equal(t, "network.habitat.post", collection.String())
	require.Equal(t, "rkey-1", rkey.String())

	// Verify Path returns collection/rkey
	require.Equal(t, "network.habitat.post/rkey-1", uri.Path())

	// Verify Normalize round-trips cleanly
	require.Equal(t, uri, uri.Normalize())
}

func TestParseHabitatURI(t *testing.T) {
	t.Run("valid URI with all parts", func(t *testing.T) {
		uri, err := ParseHabitatURI("habitat://did:plc:abc123/network.habitat.post/rkey-1")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", uri.Authority().String())
		require.Equal(t, "network.habitat.post", uri.Collection().String())
		require.Equal(t, "rkey-1", string(uri.RecordKey()))
		require.Equal(t, "network.habitat.post/rkey-1", uri.Path())
		require.Equal(t, uri, uri.Normalize())
	})

	t.Run("valid URI with authority only", func(t *testing.T) {
		uri, err := ParseHabitatURI("habitat://did:plc:abc123")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", uri.Authority().String())
		require.Equal(t, "", uri.Path())
		require.Equal(t, HabitatURI("habitat://did:plc:abc123"), uri.Normalize())
	})

	t.Run("invalid URI", func(t *testing.T) {
		_, err := ParseHabitatURI("not-a-uri")
		require.Error(t, err)
	})
}

func TestHabitatClique(t *testing.T) {
	t.Run("valid clique", func(t *testing.T) {
		uri, err := ParseHabitatClique("habitat://did:plc:abc123/network.habitat.clique/clique-rkey")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", uri.Authority().String())
		require.Equal(t, "network.habitat.clique", uri.Collection().String())
		require.Equal(t, "clique-rkey", string(uri.RecordKey()))
		require.Equal(t, "network.habitat.clique/clique-rkey", uri.Path())
		require.Equal(t, uri, uri.Normalize())
	})

	t.Run("invalid clique", func(t *testing.T) {
		_, err := ParseHabitatClique("habitat://did:plc:abc123/network.habitat.not.a.clique/clique-rkey")
		require.Error(t, err)
	})
}
