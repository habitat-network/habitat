package syntax

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConstructClique(t *testing.T) {
	clique := ConstructClique("did:plc:abc123", "mykey")
	require.Equal(t, Clique("clique:did:plc:abc123/mykey"), clique)

	// Verify the parts round-trip correctly
	require.Equal(t, "did:plc:abc123", clique.Authority().String())
	require.Equal(t, "mykey", clique.Key())
}

func TestParseClique(t *testing.T) {
	t.Run("valid clique", func(t *testing.T) {
		clique, err := ParseClique("clique:did:plc:abc123/mykey")
		require.NoError(t, err)
		require.Equal(t, "did:plc:abc123", clique.Authority().String())
		require.Equal(t, "mykey", clique.Key())
		require.Equal(t, "clique:did:plc:abc123/mykey", clique.String())
	})

	t.Run("valid clique with did:web authority", func(t *testing.T) {
		clique, err := ParseClique("clique:did:web:example.com/somekey")
		require.NoError(t, err)
		require.Equal(t, "did:web:example.com", clique.Authority().String())
		require.Equal(t, "somekey", clique.Key())
	})

	t.Run("valid clique with key containing hyphens and dots", func(t *testing.T) {
		clique, err := ParseClique("clique:did:plc:abc123/my-key.1")
		require.NoError(t, err)
		require.Equal(t, "my-key.1", clique.Key())
	})

	t.Run("missing key segment", func(t *testing.T) {
		_, err := ParseClique("clique:did:plc:abc123")
		require.Error(t, err)
	})

	t.Run("missing clique scheme", func(t *testing.T) {
		_, err := ParseClique("did:plc:abc123/mykey")
		require.Error(t, err)
	})

	t.Run("invalid DID authority", func(t *testing.T) {
		_, err := ParseClique("clique:notadid/mykey")
		require.Error(t, err)
	})

	t.Run("too long", func(t *testing.T) {
		long := "clique:did:plc:abc123/" + string(make([]byte, 8192))
		_, err := ParseClique(long)
		require.Error(t, err)
	})

	t.Run("empty string", func(t *testing.T) {
		_, err := ParseClique("")
		require.Error(t, err)
	})
}

func TestCliqueTextMarshaling(t *testing.T) {
	clique := Clique("clique:did:plc:abc123/mykey")

	text, err := clique.MarshalText()
	require.NoError(t, err)
	require.Equal(t, "clique:did:plc:abc123/mykey", string(text))

	var parsed Clique
	err = parsed.UnmarshalText([]byte("clique:did:plc:abc123/mykey"))
	require.NoError(t, err)
	require.Equal(t, clique, parsed)
}

func TestCliqueUnmarshalTextInvalid(t *testing.T) {
	var clique Clique
	err := clique.UnmarshalText([]byte("not-a-clique"))
	require.Error(t, err)
}
