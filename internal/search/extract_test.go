package search

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestExtractText(t *testing.T) {
	record := map[string]any{
		"$type":     "network.habitat.note",
		"title":     "Quarterly budget",
		"createdAt": "2026-09-29T10:00:00.000Z",
		"author":    "did:plc:abc123",
		"parent":    "at://did:plc:abc123/network.habitat.note/3k",
		"count":     int64(3),
		"pinned":    true,
		"body": map[string]any{
			"text": "review the plan",
			"tags": []any{"finance", "q3"},
			"file": map[string]any{"$type": "blob", "ref": map[string]any{"$link": "bafy"}},
		},
		"nothing": nil,
	}
	// Keys are visited in sorted order: body.file has no text, body.tags,
	// body.text, then title.
	require.Equal(t, "finance q3 review the plan Quarterly budget", ExtractText(record))
}

func TestExtractTextSanitizes(t *testing.T) {
	require.Equal(t, "ab", ExtractText(map[string]any{"x": "a\x00b"}))
	require.Equal(t, "ab", ExtractText(map[string]any{"x": "a\xffb"}))
	require.Empty(t, ExtractText(map[string]any{}))
}

func TestExtractTextIsCappedOnRuneBoundary(t *testing.T) {
	text := ExtractText(map[string]any{"x": strings.Repeat("é", maxTextBytes)})
	require.LessOrEqual(t, len(text), maxTextBytes)
	require.True(t, utf8.ValidString(text))
}
