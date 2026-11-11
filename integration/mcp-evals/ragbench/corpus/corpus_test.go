package corpus

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/internal/benchtest"
)

var allSources = []Source{SourceSlack, SourceGoogleDrive, SourceGitHub}

func docIDs(docs []*Doc) []DocID {
	ids := make([]DocID, len(docs))
	for i, doc := range docs {
		ids[i] = doc.ID
	}
	return ids
}

func TestOpenRejectsADirectoryWithoutTheDataset(t *testing.T) {
	_, err := Open(t.TempDir())
	require.ErrorIs(t, err, ErrNoDataset)
}

func TestSliceTakesAnswerableQuestionsOfItsSources(t *testing.T) {
	dataset, err := Open(benchtest.Redwood(t))
	require.NoError(t, err)
	require.Len(t, dataset.Questions(), 4)

	// Only Slack: the other apps' questions, and the one no document
	// answers, are left out, along with their documents.
	slice, err := dataset.Slice(SliceOptions{Sources: []Source{SourceSlack}})
	require.NoError(t, err)
	require.Len(t, slice.Questions, 1)
	require.Equal(t, QuestionID("qst_0001"), slice.Questions[0].ID)
	require.Equal(t, []DocID{"dsid_a1"}, docIDs(slice.Docs))

	slice, err = dataset.Slice(SliceOptions{Sources: allSources, Types: []QuestionType{"semantic"}})
	require.NoError(t, err)
	require.Len(t, slice.Questions, 1)
	require.Equal(t, []DocID{"dsid_c1"}, docIDs(slice.Docs))
}

func TestSliceAddsSiblingsAndDistractors(t *testing.T) {
	dataset, err := Open(benchtest.Redwood(t))
	require.NoError(t, err)

	// A sibling comes from the answering document's own channel.
	slice, err := dataset.Slice(SliceOptions{Sources: []Source{SourceSlack}, Siblings: 5})
	require.NoError(t, err)
	require.Equal(t, []DocID{"dsid_a1", "dsid_a2"}, docIDs(slice.Docs))

	// Distractors come from anywhere in the source.
	slice, err = dataset.Slice(SliceOptions{Sources: []Source{SourceSlack}, Distractors: 5})
	require.NoError(t, err)
	require.Equal(t, []DocID{"dsid_a1", "dsid_a2", "dsid_a3"}, docIDs(slice.Docs))

	// The same seed picks the same documents.
	opts := SliceOptions{Sources: allSources, MaxQuestions: 2, Distractors: 1, Seed: 7}
	first, err := dataset.Slice(opts)
	require.NoError(t, err)
	second, err := dataset.Slice(opts)
	require.NoError(t, err)
	require.Len(t, first.Questions, 2)
	require.Equal(t, first.Questions, second.Questions)
	require.Equal(t, docIDs(first.Docs), docIDs(second.Docs))
}

func TestSliceReadsDocumentsAndFindsThemByURL(t *testing.T) {
	dataset, err := Open(benchtest.Redwood(t))
	require.NoError(t, err)
	slice, err := dataset.Slice(SliceOptions{Sources: allSources})
	require.NoError(t, err)

	pr := slice.Doc("dsid_b1")
	require.Equal(t, SourceGitHub, pr.Source)
	require.Equal(t, "Add multipart upload limits", pr.Title)
	require.Contains(t, pr.Text, "10 MiB")
	require.Equal(t, "https://github.com/redwood-inference/redwood/pull/101", pr.URL)
	thread := slice.Doc("dsid_a1")
	require.Equal(t, "incidents", thread.Title)
	require.Equal(t, "ava\ndiego", thread.Field("participants"))
	require.Equal(t,
		"https://redwood-inference.slack.com/archives/incidents/p1700000001", thread.URL)
	require.Same(t, pr, slice.DocByURL(pr.URL))
	require.Nil(t, slice.Doc("dsid_a3"), "not an answer, and no distractors were asked for")

	// Documents are recognized by their URLs in a search tool's text.
	text := "Permalink: " + thread.URL + "\nsee also (" + pr.URL + "), " + pr.URL +
		" and https://example.com/other."
	require.Equal(t, []DocID{"dsid_a1", "dsid_b1"}, slice.Mentioned(text))
}

func TestSliceGivesDocumentsSharingAURLDifferentOnes(t *testing.T) {
	// Near-duplicates: the same PR number in the same repository.
	dir := benchtest.Write(t, []benchtest.Doc{
		benchtest.PullRequest("dsid_11111111aa", "redwood", "5", "Original", "first"),
		{ID: "dsid_22222222bb", Path: "github/redwood/pr-5-copy.json",
			Fields: benchtest.PullRequest("", "redwood", "5", "Copy", "second").Fields},
	}, []benchtest.Question{{
		ID: "q1", Type: "basic", Source: "github", Text: "which?",
		Docs: []string{"dsid_11111111aa", "dsid_22222222bb"},
	}})
	dataset, err := Open(dir)
	require.NoError(t, err)
	slice, err := dataset.Slice(SliceOptions{Sources: allSources})
	require.NoError(t, err)

	first, second := slice.Doc("dsid_11111111aa"), slice.Doc("dsid_22222222bb")
	require.NotEqual(t, first.URL, second.URL)
	require.Same(t, first, slice.DocByURL(first.URL))
	require.Same(t, second, slice.DocByURL(second.URL))
}

func TestSliceFailsWhenAnAnsweringDocumentIsMissing(t *testing.T) {
	dir := benchtest.Write(t, nil, nil)
	dataset, err := Open(dir)
	require.NoError(t, err)
	dataset.paths["dsid_gone"] = "slack/eng/gone.json"
	dataset.questions = []Question{{ID: "q1", ExpectedDocs: []DocID{"dsid_gone"}}}

	_, err = dataset.Slice(SliceOptions{Sources: allSources})
	require.ErrorContains(t, err, "dsid_gone")
}

func TestIndexSearchRanksAndMatches(t *testing.T) {
	dataset, err := Open(benchtest.Redwood(t))
	require.NoError(t, err)
	slice, err := dataset.Slice(SliceOptions{Sources: allSources, Distractors: 10})
	require.NoError(t, err)
	index := NewIndex(slice.Docs)

	// Any word matches, and the document with the most of them ranks first.
	hits := index.Search("What is the multipart upload file size limit?", MatchAny, 3, nil)
	require.NotEmpty(t, hits)
	require.Equal(t, DocID("dsid_b1"), hits[0].Doc.ID)
	require.LessOrEqual(t, len(hits), 3)

	// Every word (stop words aside) has to match: a whole question rarely
	// does, its keywords do.
	require.Empty(
		t,
		index.Search("What is the multipart upload file size limit?", MatchAll, 3, nil),
	)
	hits = index.Search("what is the multipart upload", MatchAll, 3, nil)
	require.Equal(t, []DocID{"dsid_b1"}, hitIDs(hits))

	// Metadata is searchable, and a filter applies before ranking.
	hits = index.Search("installer", MatchAny, 3, nil)
	require.Equal(t, []DocID{"dsid_b3"}, hitIDs(hits))
	hits = index.Search("redwood", MatchAny, 10, func(doc *Doc) bool { return doc.ID != "dsid_b1" })
	require.Equal(t, []DocID{"dsid_b2"}, hitIDs(hits))

	require.Empty(t, index.Search("zebra", MatchAny, 3, nil))
	require.Empty(t, NewIndex(nil).Search("anything", MatchAny, 3, nil))
}

func hitIDs(hits []Hit) []DocID {
	ids := make([]DocID, len(hits))
	for i, hit := range hits {
		ids[i] = hit.Doc.ID
	}
	return ids
}
