package utils

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestParseBytesCoversLexiconJSON covers ParseBytes against the lexicon
// bytes field's JSON representation and its error paths.
func TestParseBytesCoversLexiconJSON(t *testing.T) {
	t.Parallel()

	b, err := ParseBytes(nil)
	require.NoError(t, err)
	require.Nil(t, b)

	b, err = ParseBytes(map[string]any{
		"$bytes": base64.RawStdEncoding.EncodeToString([]byte{1, 2}),
	})
	require.NoError(t, err)
	require.Equal(t, []byte{1, 2}, b)

	_, err = ParseBytes(42)
	require.ErrorIs(t, err, ErrInvalidBytesField)

	_, err = ParseBytes(map[string]any{"$bytes": 42})
	require.ErrorIs(t, err, ErrInvalidBytesField)

	_, err = ParseBytes(map[string]any{"$bytes": "!!!notbase64!!!"})
	require.ErrorIs(t, err, ErrInvalidBytesField)
}
