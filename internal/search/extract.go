package search

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// maxTextBytes caps the text indexed for one record.
const maxTextBytes = 64 << 10

// ExtractText flattens the human-readable strings of a decoded record (as
// returned by atdata.UnmarshalCBOR) into one space-separated string for
// indexing.
//
// Object keys are visited in sorted order so the result is deterministic.
// Keys starting with "$" ($type, $link, $bytes) are skipped, as are strings
// that are identifiers rather than prose: DIDs, at:// URIs and datetimes.
// Numbers, booleans and bytes are ignored.
func ExtractText(record map[string]any) string {
	var sb strings.Builder
	walk(record, &sb)
	text := strings.TrimSpace(sb.String())
	if len(text) > maxTextBytes {
		text = truncateUTF8(text, maxTextBytes)
	}
	return text
}

// truncateUTF8 cuts text to at most limit bytes on a rune boundary, so the tail
// stays valid UTF-8.
func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}

func walk(v any, sb *strings.Builder) {
	if sb.Len() >= maxTextBytes {
		return
	}
	switch val := v.(type) {
	case string:
		if s := clean(val); s != "" && !isIdentifier(s) {
			sb.WriteString(s)
			sb.WriteByte(' ')
		}
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			if !strings.HasPrefix(k, "$") {
				keys = append(keys, k)
			}
		}
		slices.Sort(keys)
		for _, k := range keys {
			walk(val[k], sb)
		}
	case []any:
		for _, child := range val {
			walk(child, sb)
		}
	}
}

// clean drops NUL bytes, which Postgres text can't hold, and invalid UTF-8.
func clean(s string) string {
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "")
	}
	return strings.TrimSpace(strings.ReplaceAll(s, "\x00", ""))
}

func isIdentifier(s string) bool {
	if strings.HasPrefix(s, "at://") {
		return true
	}
	if strings.HasPrefix(s, "did:") {
		if _, err := syntax.ParseDID(s); err == nil {
			return true
		}
	}
	_, err := syntax.ParseDatetime(s)
	return err == nil
}
