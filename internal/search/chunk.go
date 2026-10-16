package search

import (
	"strings"
)

const (
	// chunkSize is about how many bytes of text each stored chunk holds, so
	// a long text is ranked and embedded piece by piece rather than as one
	// blob of which the embedder only sees the opening.
	chunkSize = 2000
	// chunkOverlap is about how many bytes of the end of one chunk start the
	// next, so a phrase cut at a boundary still matches.
	chunkOverlap = 200
	// maxChunks caps the chunks of one text.
	maxChunks = 500
)

// chunkSeparators are where chunkText prefers to split, best first:
// paragraphs, lines, sentences, then words.
var chunkSeparators = []string{"\n\n", "\n", ". ", " "}

// chunkText splits text into chunks of about [chunkSize] bytes at the most
// logical boundaries it can, each starting with the end of the previous
// one. Text that fits in one chunk is returned whole, and empty text has no
// chunks.
func chunkText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if len(text) <= chunkSize {
		return []string{text}
	}
	var chunks []string
	var cur strings.Builder
	flush := func() {
		chunks = append(chunks, cur.String())
		tail := overlapTail(cur.String())
		cur.Reset()
		cur.WriteString(tail)
	}
	// overlapped is how much of cur is the previous chunk's tail.
	overlapped := 0
	for _, piece := range splitPieces(text, chunkSeparators) {
		if cur.Len() > overlapped && cur.Len()+1+len(piece) > chunkSize {
			flush()
			overlapped = cur.Len()
			if len(chunks) == maxChunks {
				return chunks
			}
		}
		if cur.Len() > 0 {
			cur.WriteByte(' ')
		}
		cur.WriteString(piece)
	}
	if cur.Len() > overlapped {
		chunks = append(chunks, cur.String())
	}
	return chunks
}

// splitPieces splits text at the first of seps, splitting any piece still
// longer than [chunkSize] at the next, and cutting pieces with no separator
// left at a rune boundary.
func splitPieces(text string, seps []string) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}
	if len(seps) == 0 {
		var out []string
		for len(text) > chunkSize {
			cut := truncateUTF8(text, chunkSize)
			out = append(out, cut)
			text = text[len(cut):]
		}
		return append(out, text)
	}
	var out []string
	for _, part := range strings.SplitAfter(text, seps[0]) {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, splitPieces(part, seps[1:])...)
		}
	}
	return out
}

// overlapTail returns about the last [chunkOverlap] bytes of chunk, starting
// at a word.
func overlapTail(chunk string) string {
	if len(chunk) <= chunkOverlap {
		return ""
	}
	tail := chunk[len(chunk)-chunkOverlap:]
	if i := strings.IndexByte(tail, ' '); i >= 0 {
		return tail[i+1:]
	}
	// One long word: no clean place to start, so don't overlap.
	return ""
}
