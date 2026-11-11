package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
)

// scores are the metrics of a group of samples.
type scores struct {
	Group     string
	Questions int
	// Recall is the mean share of a question's answering documents that
	// get_context returned; HitRate the share of questions it returned at
	// least one for; MRR the mean reciprocal rank of the first.
	Recall, HitRate, MRR float64
	// DirectRecall and IndexRecall are the same recall for the yardsticks
	// (Sample.DirectContextIDs and IndexContextIDs).
	DirectRecall, IndexRecall float64
	// ToolHit1, ToolHit3 and ToolHit5 are the share of questions with a
	// reference tool among the first 1, 3 and 5 suggestions; ToolMRR the
	// mean reciprocal rank of the first; AppHit3 the share with any tool of
	// the right app in the first 3.
	ToolHit1, ToolHit3, ToolHit5, ToolMRR, AppHit3 float64
}

func score(group string, samples []Sample) scores {
	var recall, hit, mrr, direct, index, t1, t3, t5, tmrr, app3 []float64
	for _, s := range samples {
		want := s.ReferenceContextIDs
		recall = append(recall, recallAt(s.RetrievedContextIDs, want, 0))
		hit = append(hit, flag01(hitAt(s.RetrievedContextIDs, want, len(s.RetrievedContextIDs))))
		mrr = append(mrr, reciprocalRank(s.RetrievedContextIDs, want))
		direct = append(direct, recallAt(s.DirectContextIDs, want, 0))
		index = append(index, recallAt(s.IndexContextIDs, want, 0))

		tools := make([]string, len(s.SuggestedTools))
		apps := make([]string, len(s.SuggestedTools))
		for i, tool := range s.SuggestedTools {
			tools[i] = tool.Name
			apps[i], _, _ = strings.Cut(tool.Name, ":")
		}
		wantApps := make([]string, len(s.ReferenceTools))
		for i, tool := range s.ReferenceTools {
			wantApps[i], _, _ = strings.Cut(tool, ":")
		}
		t1 = append(t1, flag01(hitAt(tools, s.ReferenceTools, 1)))
		t3 = append(t3, flag01(hitAt(tools, s.ReferenceTools, 3)))
		t5 = append(t5, flag01(hitAt(tools, s.ReferenceTools, 5)))
		tmrr = append(tmrr, reciprocalRank(tools, s.ReferenceTools))
		app3 = append(app3, flag01(hitAt(apps, wantApps, 3)))
	}
	return scores{
		Group: group, Questions: len(samples),
		Recall: mean(recall), HitRate: mean(hit), MRR: mean(mrr),
		DirectRecall: mean(direct), IndexRecall: mean(index),
		ToolHit1: mean(t1), ToolHit3: mean(t3), ToolHit5: mean(t5), ToolMRR: mean(tmrr),
		AppHit3: mean(app3),
	}
}

func flag01(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// groupScores scores samples overall, then by question type, then by the app
// holding the answer.
func groupScores(samples []Sample) []scores {
	byType := make(map[string][]Sample)
	bySource := make(map[string][]Sample)
	for _, s := range samples {
		byType["type: "+string(s.QuestionType)] = append(byType["type: "+string(s.QuestionType)], s)
		sources := make([]string, len(s.Sources))
		for i, source := range s.Sources {
			sources[i] = string(source)
		}
		key := "source: " + strings.Join(sources, "+")
		bySource[key] = append(bySource[key], s)
	}
	out := []scores{score("all", samples)}
	for _, groups := range []map[string][]Sample{byType, bySource} {
		var rows []scores
		for group, samples := range groups {
			rows = append(rows, score(group, samples))
		}
		slices.SortFunc(rows, func(a, b scores) int {
			return cmp.Or(cmp.Compare(b.Questions, a.Questions), cmp.Compare(a.Group, b.Group))
		})
		out = append(out, rows...)
	}
	return out
}

func pct(x float64) string { return fmt.Sprintf("%.0f%%", 100*x) }

// summary renders a run's report as Markdown.
func summary(settings []string, slice *corpus.Slice, samples []Sample) string {
	var ok []Sample
	var failed []Sample
	var durations []float64
	notes := make(map[string]int)
	for _, s := range samples {
		if s.Error != "" {
			failed = append(failed, s)
			continue
		}
		ok = append(ok, s)
		durations = append(durations, float64(s.DurationMS))
		for _, note := range s.Notes {
			notes[note]++
		}
	}
	rows := groupScores(ok)

	var b strings.Builder
	b.WriteString("# ragbench: get_context retrieval and tool suggestion\n\n")
	for _, line := range settings {
		fmt.Fprintf(&b, "- %s\n", line)
	}
	fmt.Fprintf(&b, "- slice: %s\n", slice)

	b.WriteString("\n## Retrieval\n\n" +
		"Recall is the share of a question's answering documents found. `get_context` is what " +
		"pear returned; `direct` asks each app's search for the question itself, " +
		fmt.Sprintf("%d results from each; `one index` is the best %d of a single index over ",
			directLimit, indexLimit) +
		"every app's documents.\n\n" +
		"| | questions | get_context recall | hit rate | MRR | direct recall | one index recall |\n" +
		"|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %.2f | %s | %s |\n", r.Group, r.Questions,
			pct(r.Recall), pct(r.HitRate), r.MRR, pct(r.DirectRecall), pct(r.IndexRecall))
	}

	b.WriteString("\n## Tool suggestion\n\n" +
		"A question's reference tool is the search tool of the app holding its answer. hit@k " +
		"is the share of questions with it among the first k suggestions; `right app @3` " +
		"counts any tool of that app.\n\n" +
		"| | questions | hit@1 | hit@3 | hit@5 | MRR | right app @3 |\n" +
		"|---|---|---|---|---|---|---|\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %.2f | %s |\n", r.Group, r.Questions,
			pct(r.ToolHit1), pct(r.ToolHit3), pct(r.ToolHit5), r.ToolMRR, pct(r.AppHit3))
	}

	fmt.Fprintf(&b, "\n## Health\n\n- get_context failed on %d of %d questions\n"+
		"- latency: p50 %.0f ms, p95 %.0f ms\n",
		len(failed), len(samples), percentile(durations, 0.5), percentile(durations, 0.95))
	for _, note := range slices.Sorted(mapKeys(notes)) {
		fmt.Fprintf(&b, "- note on %d questions: %s\n", notes[note], note)
	}

	// The questions where pear lost a document the apps themselves found
	// point at what pear does to a query or a result on the way.
	var lost []string
	for _, s := range ok {
		for _, id := range s.ReferenceContextIDs {
			if slices.Contains(s.DirectContextIDs, id) &&
				!slices.Contains(s.RetrievedContextIDs, id) {
				lost = append(lost, string(s.QuestionID))
				break
			}
		}
	}
	fmt.Fprintf(&b, "\n## Lost on the way\n\n%d questions where searching the apps directly "+
		"found an answering document that get_context didn't return", len(lost))
	if len(lost) > 0 {
		fmt.Fprintf(&b, ": %s", strings.Join(lost, ", "))
	}
	b.WriteString(".\n")
	return b.String()
}

func mapKeys(m map[string]int) func(yield func(string) bool) {
	return func(yield func(string) bool) {
		for k := range m {
			if !yield(k) {
				return
			}
		}
	}
}
