package search

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestChunkTextKeepsShortTextWhole(t *testing.T) {
	require.Nil(t, chunkText("  \n "))
	require.Equal(t, []string{"a short note"}, chunkText(" a short note\n"))
}

func TestChunkTextSplitsAtParagraphsWithOverlap(t *testing.T) {
	para := strings.Repeat("word ", 150) // 750 bytes
	var paras []string
	for range 8 {
		paras = append(paras, strings.TrimSpace(para))
	}
	chunks := chunkText(strings.Join(paras, "\n\n"))
	require.Greater(t, len(chunks), 2)
	for i, c := range chunks {
		require.LessOrEqual(t, len(c), chunkSize+chunkOverlap)
		if i > 0 {
			// Each chunk starts with the end of the one before.
			tail := overlapTail(chunks[i-1])
			require.NotEmpty(t, tail)
			require.True(t, strings.HasPrefix(c, tail))
		}
	}
}

func TestChunkTextCapsChunks(t *testing.T) {
	chunks := chunkText(strings.Repeat("word ", maxChunks*chunkSize/4))
	require.Len(t, chunks, maxChunks)
}

func TestChunkTextCutsLongWordsOnRunes(t *testing.T) {
	chunks := chunkText(strings.Repeat("é", chunkSize))
	require.Len(t, chunks, 2)
	for _, c := range chunks {
		require.True(t, utf8.ValidString(c))
		require.LessOrEqual(t, len(c), chunkSize)
	}
}

func TestRecordDatePrefersUpdatedAt(t *testing.T) {
	require.Equal(t,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC),
		recordDate(map[string]any{
			"createdAt": "2026-01-01T00:00:00Z",
			"updatedAt": "2026-02-01T00:00:00Z",
		}).UTC())
	require.Equal(t,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		recordDate(map[string]any{"createdAt": "2026-01-01T00:00:00Z", "updatedAt": "soon"}).UTC())
	require.True(t, recordDate(map[string]any{"text": "hi"}).IsZero())
}
