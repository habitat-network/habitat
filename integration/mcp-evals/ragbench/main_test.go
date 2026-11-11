package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/internal/benchtest"
)

func readSamples(t *testing.T, dir string) map[corpus.QuestionID]Sample {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "samples.jsonl"))
	require.NoError(t, err)
	defer func() { _ = f.Close() }()
	samples := make(map[corpus.QuestionID]Sample)
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		var s Sample
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &s))
		samples[s.QuestionID] = s
	}
	require.NoError(t, scanner.Err())
	return samples
}

func TestRetrievalWritesASampleForEveryQuestion(t *testing.T) {
	out := t.TempDir()

	err := run([]string{
		"retrieval", "-dataset", benchtest.Redwood(t), "-distractors", "10", "-out", out,
	})
	require.NoError(t, err)

	// The three answerable questions, each with its answering document
	// found through pear, and by both yardsticks.
	samples := readSamples(t, out)
	require.Len(t, samples, 3)
	for id, want := range map[corpus.QuestionID]struct {
		doc  corpus.DocID
		tool string
	}{
		"qst_0001": {"dsid_a1", "slack:slack_search_public_and_private"},
		"qst_0002": {"dsid_b1", "github:search_pull_requests"},
		"qst_0003": {"dsid_c1", "google-drive:search_files"},
	} {
		s := samples[id]
		require.Empty(t, s.Error, id)
		require.Equal(t, []corpus.DocID{want.doc}, s.ReferenceContextIDs, id)
		require.Contains(t, s.RetrievedContextIDs, want.doc, id)
		require.Contains(t, s.DirectContextIDs, want.doc, id)
		require.Contains(t, s.IndexContextIDs, want.doc, id)
		require.Equal(t, []string{want.tool}, s.ReferenceTools, id)
		require.NotEmpty(t, s.RetrievedContexts, id)
		require.NotEmpty(t, s.MockCalls, id)
		require.NotEmpty(t, s.Reference, id)
	}

	summary, err := os.ReadFile(filepath.Join(out, "summary.md"))
	require.NoError(t, err)
	require.Contains(t, string(summary), "| all | 3 | 100% | 100% |")
	require.Contains(t, string(summary), "| source: slack | 1 |")
	require.Contains(t, string(summary), "get_context failed on 0 of 3 questions")
}

func TestRetrievalHonorsAppsAndMaxTools(t *testing.T) {
	out := t.TempDir()

	err := run([]string{
		"retrieval", "-dataset", benchtest.Redwood(t), "-distractors", "10", "-out", out,
		"-match", "all", "-apps", "slack", "-max-tools", "0",
	})
	require.NoError(t, err)

	samples := readSamples(t, out)
	require.Len(t, samples, 1)
	s := samples["qst_0001"]
	require.Empty(t, s.Error)
	require.Empty(t, s.SuggestedTools)
	for _, call := range s.MockCalls {
		require.Equal(t, "slack", string(call.App))
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	dataset := benchtest.Redwood(t)
	for name, args := range map[string][]string{
		"no command":       nil,
		"unknown command":  {"score"},
		"no dataset":       {"retrieval", "-dataset", ""},
		"not a dataset":    {"retrieval", "-dataset", t.TempDir()},
		"unknown app":      {"retrieval", "-dataset", dataset, "-apps", "jira"},
		"unknown match":    {"retrieval", "-dataset", dataset, "-match", "fuzzy"},
		"unknown selector": {"retrieval", "-dataset", dataset, "-selector", "random"},
		"decision without url": {
			"retrieval", "-dataset", dataset, "-selector", "decision", "-decisions-url", "",
		},
		"no questions": {"retrieval", "-dataset", dataset, "-types", "high_level"},
	} {
		require.Error(t, run(args), name)
	}
}

func TestMetrics(t *testing.T) {
	got := []corpus.DocID{"a", "b", "c", "d"}

	require.InDelta(t, 0.5, recallAt(got, []corpus.DocID{"b", "z"}, 0), 1e-9)
	require.InDelta(t, 0.0, recallAt(got, []corpus.DocID{"c"}, 2), 1e-9)
	require.InDelta(t, 1.0, recallAt(got, []corpus.DocID{"c"}, 3), 1e-9)
	require.Zero(t, recallAt(got, nil, 0))

	require.InDelta(t, 1.0/3, reciprocalRank(got, []corpus.DocID{"z", "c"}), 1e-9)
	require.Zero(t, reciprocalRank(got, []corpus.DocID{"z"}))

	require.True(t, hitAt(got, []corpus.DocID{"b"}, 2))
	require.False(t, hitAt(got, []corpus.DocID{"c"}, 2))

	require.InDelta(t, 2.0, mean([]float64{1, 2, 3}), 1e-9)
	require.Zero(t, mean(nil))
	require.InDelta(t, 3.0, percentile([]float64{5, 1, 3, 2, 4}, 0.5), 1e-9)
	require.InDelta(t, 5.0, percentile([]float64{5, 1, 3, 2, 4}, 0.95), 1e-9)
	require.Zero(t, percentile(nil, 0.5))
}

func TestSummaryListsQuestionsLostOnTheWay(t *testing.T) {
	samples := []Sample{
		{
			QuestionID: "q1", QuestionType: "basic", Sources: []corpus.Source{corpus.SourceSlack},
			ReferenceContextIDs: []corpus.DocID{"a"}, DirectContextIDs: []corpus.DocID{"a"},
			SuggestedTools: []SuggestedTool{{Name: "slack:other"}, {Name: "slack:search"}},
			ReferenceTools: []string{"slack:search"}, Notes: []string{"github: search timeout"},
		},
		{QuestionID: "q2", Error: "boom"},
	}

	report := summary([]string{"apps: slack"}, &corpus.Slice{}, samples)

	require.Contains(t, report, "- apps: slack\n")
	// Found directly but not by get_context: recall 0 against 100%.
	require.Contains(t, report, "| all | 1 | 0% | 0% | 0.00 | 100% | 0% |")
	// The right tool was second: no hit@1, hit@3, MRR 0.5, right app first.
	require.Contains(t, report, "| all | 1 | 0% | 100% | 100% | 0.50 | 100% |")
	require.Contains(t, report, "get_context failed on 1 of 2 questions")
	require.Contains(t, report, "note on 1 questions: github: search timeout")
	require.Contains(t, report, "1 questions where searching the apps directly found an "+
		"answering document that get_context didn't return: q1.")
}
