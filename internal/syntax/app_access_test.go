package syntax

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAppAccessRkey(t *testing.T) {
	const clientID = "https://app.example.com/client-metadata.json"

	t.Run("is deterministic", func(t *testing.T) {
		first, err := AppAccessRkey(clientID)
		require.NoError(t, err)
		second, err := AppAccessRkey(clientID)
		require.NoError(t, err)
		require.Equal(t, first, second)
	})

	t.Run("round-trips back to the client id", func(t *testing.T) {
		rkey, err := AppAccessRkey(clientID)
		require.NoError(t, err)
		decoded, err := base64.RawURLEncoding.DecodeString(rkey.String())
		require.NoError(t, err)
		require.Equal(t, clientID, string(decoded))
	})

	t.Run("distinct client ids produce distinct rkeys", func(t *testing.T) {
		rkey, err := AppAccessRkey(clientID)
		require.NoError(t, err)
		other, err := AppAccessRkey("https://other.example.com/client-metadata.json")
		require.NoError(t, err)
		require.NotEqual(t, rkey, other)
	})

	t.Run("rejects an empty client id", func(t *testing.T) {
		_, err := AppAccessRkey("")
		require.Error(t, err)
	})

	t.Run("rejects a client id too long to address as a record key", func(t *testing.T) {
		// base64url expansion of a 1000-byte client_id is well past the
		// atproto record key's 512-char limit.
		_, err := AppAccessRkey(strings.Repeat("a", 1000))
		require.Error(t, err)
	})
}
