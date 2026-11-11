package corpus

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
)

// SliceOptions choose the questions and documents of a Slice.
type SliceOptions struct {
	// Sources are the apps to take questions and documents from. A question
	// is taken only if every document answering it is in one of them.
	Sources []Source
	// Types limits the questions to these categories; empty takes all.
	Types []QuestionType
	// MaxQuestions caps how many questions are taken, picked at random
	// (by Seed); zero takes all.
	MaxQuestions int
	// Siblings is how many documents to take from the same directory (Slack
	// channel, repository, Drive folder) as each answering document. Those
	// are the hard distractors: same topic, and where the dataset puts its
	// near-duplicates.
	Siblings int
	// Distractors is how many other documents to take from each source, at
	// random.
	Distractors int
	// Seed makes the random picks repeatable.
	Seed uint64
}

// Slice is the part of the dataset one run works with: some questions, the
// documents answering them, and distractors.
type Slice struct {
	// Questions are in the dataset's order.
	Questions []Question
	// Docs are ordered by id.
	Docs  []*Doc
	byID  map[DocID]*Doc
	byURL map[string]*Doc
}

// Slice picks questions and documents by opts and reads the documents from
// disk.
func (d *Dataset) Slice(opts SliceOptions) (*Slice, error) {
	// Each kind of pick has its own stream, so changing how many siblings
	// are taken doesn't change which questions are.
	rng := func(stream uint64) *rand.Rand { return rand.New(rand.NewPCG(opts.Seed, stream)) }

	var questions []Question
	for _, q := range d.questions {
		if len(q.ExpectedDocs) == 0 ||
			(len(opts.Types) > 0 && !slices.Contains(opts.Types, q.Type)) ||
			slices.ContainsFunc(q.ExpectedDocs, func(id DocID) bool {
				return !slices.Contains(opts.Sources, sourceOf(d.paths[id]))
			}) {
			continue
		}
		questions = append(questions, q)
	}
	if opts.MaxQuestions > 0 && len(questions) > opts.MaxQuestions {
		keep := rng(1).Perm(len(questions))[:opts.MaxQuestions]
		slices.Sort(keep)
		picked := make([]Question, len(keep))
		for i, k := range keep {
			picked[i] = questions[k]
		}
		questions = picked
	}

	// Sorted, since picks must not depend on map order.
	bySource := make(map[Source][]DocID)
	byDir := make(map[string][]DocID)
	for id, p := range d.paths {
		if source := sourceOf(p); slices.Contains(opts.Sources, source) {
			bySource[source] = append(bySource[source], id)
			byDir[dirOf(p)] = append(byDir[dirOf(p)], id)
		}
	}
	for _, ids := range bySource {
		slices.Sort(ids)
	}
	for _, ids := range byDir {
		slices.Sort(ids)
	}

	picked := make(map[DocID]bool)
	siblings := rng(2)
	for _, q := range questions {
		for _, id := range q.ExpectedDocs {
			picked[id] = true
			pickSome(siblings, byDir[dirOf(d.paths[id])], opts.Siblings, picked)
		}
	}
	distractors := rng(3)
	for _, source := range opts.Sources {
		pickSome(distractors, bySource[source], opts.Distractors, picked)
	}

	ids := make([]DocID, 0, len(picked))
	for id := range picked {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	docs, err := d.loadAll(ids)
	if err != nil {
		return nil, err
	}
	return newSlice(questions, docs), nil
}

// pickSome adds up to n of ids that aren't yet in picked, at random.
func pickSome(rng *rand.Rand, ids []DocID, n int, picked map[DocID]bool) {
	for _, i := range rng.Perm(len(ids)) {
		if n <= 0 {
			return
		}
		if !picked[ids[i]] {
			picked[ids[i]] = true
			n--
		}
	}
}

func (d *Dataset) loadAll(ids []DocID) ([]*Doc, error) {
	docs := make([]*Doc, len(ids))
	errs := make([]error, len(ids))
	var wg sync.WaitGroup
	next := make(chan int)
	for range runtime.GOMAXPROCS(0) {
		wg.Go(func() {
			for i := range next {
				docs[i], errs[i] = d.load(ids[i])
			}
		})
	}
	for i := range ids {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return docs, nil
}

// newSlice indexes docs (ordered by id), giving each a URL no other has.
func newSlice(questions []Question, docs []*Doc) *Slice {
	s := &Slice{
		Questions: questions, Docs: docs,
		byID: make(map[DocID]*Doc, len(docs)), byURL: make(map[string]*Doc, len(docs)),
	}
	for _, doc := range docs {
		if _, taken := s.byURL[doc.URL]; taken {
			// Near-duplicates can share a channel and timestamp, or a PR
			// number; tell them apart by a piece of their id.
			doc.URL += "-" + strings.TrimPrefix(string(doc.ID), "dsid_")[:8]
		}
		s.byID[doc.ID] = doc
		s.byURL[doc.URL] = doc
	}
	return s
}

// Doc returns the document id, or nil if the slice doesn't hold it.
func (s *Slice) Doc(id DocID) *Doc { return s.byID[id] }

// DocByURL returns the document at url, or nil.
func (s *Slice) DocByURL(url string) *Doc { return s.byURL[url] }

// OfSource returns the slice's documents from source, ordered by id.
func (s *Slice) OfSource(source Source) []*Doc {
	var out []*Doc
	for _, doc := range s.Docs {
		if doc.Source == source {
			out = append(out, doc)
		}
	}
	return out
}

var urlPattern = regexp.MustCompile(`https://[^\s"'<>()\[\]]+`)

// Mentioned returns the documents whose URLs appear in text, in the order
// they first appear. It's how search results, which carry URLs rather than
// ids, are matched back to the documents they are.
func (s *Slice) Mentioned(text string) []DocID {
	var out []DocID
	for _, url := range urlPattern.FindAllString(text, -1) {
		doc := s.byURL[strings.TrimRight(url, ".,;:!?")]
		if doc != nil && !slices.Contains(out, doc.ID) {
			out = append(out, doc.ID)
		}
	}
	return out
}

// String summarizes the slice's size.
func (s *Slice) String() string {
	counts := make(map[Source]int)
	for _, doc := range s.Docs {
		counts[doc.Source]++
	}
	sources := make([]string, 0, len(counts))
	for source, n := range counts {
		sources = append(sources, fmt.Sprintf("%s %d", source, n))
	}
	slices.Sort(sources)
	return fmt.Sprintf("%d questions, %d documents (%s)",
		len(s.Questions), len(s.Docs), strings.Join(sources, ", "))
}
