package corpus

import (
	"cmp"
	"math"
	"slices"
	"strings"
	"unicode"
)

// Match is how an Index decides which documents a query matches at all.
type Match string

const (
	// MatchAny matches documents with any of the query's words, ranked by
	// BM25: a forgiving search engine.
	MatchAny Match = "any"
	// MatchAll only matches documents with every one of the query's words
	// (stop words aside), ranked by BM25. That's closer to how the apps' own
	// keyword searches treat a query, and it punishes passing them a whole
	// sentence.
	MatchAll Match = "all"
)

// BM25's usual parameters.
const (
	bm25K1 = 1.2
	bm25B  = 0.75
	// titleWeight is how many times a title's words count.
	titleWeight = 3
)

// Index is an in-memory BM25 index over documents: the search engine behind
// the mock apps, standing in for each app's own.
type Index struct {
	docs     []*Doc
	lengths  []int
	avgLen   float64
	postings map[string][]posting
}

type posting struct {
	doc int
	tf  int
}

// Hit is a matching document and its BM25 score.
type Hit struct {
	Doc   *Doc
	Score float64
}

// NewIndex indexes docs by their title, text and the people and labels
// attached to them.
func NewIndex(docs []*Doc) *Index {
	ix := &Index{docs: docs, lengths: make([]int, len(docs)), postings: make(map[string][]posting)}
	total := 0
	for i, doc := range docs {
		counts := make(map[string]int)
		for _, w := range Words(doc.Title) {
			counts[w] += titleWeight
		}
		for _, w := range Words(doc.Text) {
			counts[w]++
		}
		for _, name := range metadataFields {
			for _, w := range Words(doc.Field(name)) {
				counts[w]++
			}
		}
		for w, tf := range counts {
			ix.postings[w] = append(ix.postings[w], posting{doc: i, tf: tf})
			ix.lengths[i] += tf
		}
		total += ix.lengths[i]
	}
	if len(docs) > 0 {
		ix.avgLen = float64(total) / float64(len(docs))
	}
	return ix
}

// metadataFields are the fields, besides title and content, a search in the
// real apps would match on.
var metadataFields = []string{
	"channel", "participants", "author", "labels", "repo", "owner", "tags", "path",
}

// Search returns the best limit documents for query, best first. keep, if
// not nil, filters documents out before ranking (a repository filter, say).
func (ix *Index) Search(query string, match Match, limit int, keep func(*Doc) bool) []Hit {
	words := Words(query)
	slices.Sort(words)
	words = slices.Compact(words)
	scores := make(map[int]float64)
	matched := make(map[int]int)
	required := 0
	for _, w := range words {
		posts := ix.postings[w]
		if !stopWords[w] {
			required++
		}
		if len(posts) == 0 {
			continue
		}
		n := float64(len(ix.docs))
		idf := math.Log(1 + (n-float64(len(posts))+0.5)/(float64(len(posts))+0.5))
		for _, p := range posts {
			tf := float64(p.tf)
			norm := 1 - bm25B + bm25B*float64(ix.lengths[p.doc])/ix.avgLen
			scores[p.doc] += idf * tf * (bm25K1 + 1) / (tf + bm25K1*norm)
			if !stopWords[w] {
				matched[p.doc]++
			}
		}
	}
	hits := make([]Hit, 0, len(scores))
	for i, score := range scores {
		if match == MatchAll && matched[i] < required {
			continue
		}
		if keep != nil && !keep(ix.docs[i]) {
			continue
		}
		hits = append(hits, Hit{Doc: ix.docs[i], Score: score})
	}
	slices.SortFunc(hits, func(a, b Hit) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.Doc.ID, b.Doc.ID))
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

// Words splits s into lower-case words of letters and digits.
func Words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// stopWords are too common to require of a matching document.
var stopWords = func() map[string]bool {
	out := make(map[string]bool)
	for w := range strings.FieldsSeq(
		"a an and are as at be been by did do does for from had has have how i in is it its " +
			"of on or our so that the their them there these they this to was we were what " +
			"when where which who whom why will with would you your",
	) {
		out[w] = true
	}
	return out
}()
