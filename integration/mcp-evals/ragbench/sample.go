package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/corpus"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/mockmcp"
	"github.com/habitat-network/habitat/integration/mcp-evals/ragbench/pearmcp"
)

// directLimit is how many results each app is asked for when it's searched
// directly, as the yardstick for pear: the same number get_context asks each
// search tool for (contextItemLimit in internal/mcpserver).
const directLimit = 5

// indexLimit is how many results the single index over every app returns.
const indexLimit = 10

// SuggestedTool is a tool get_context suggested, by the name call_tool takes
// ("github:search_pull_requests").
type SuggestedTool struct {
	Name  string  `json:"name"`
	Score float64 `json:"score"`
}

// Sample is what one question produced, one line of samples.jsonl. Its field
// names are the ones Ragas uses where it has one (see scorer/), so the
// scorer reads samples without a mapping.
type Sample struct {
	QuestionID   corpus.QuestionID   `json:"question_id"`
	QuestionType corpus.QuestionType `json:"question_type"`
	Sources      []corpus.Source     `json:"source_types"`
	// UserInput is the question, passed to get_context as its query.
	UserInput string `json:"user_input"`
	// Reference is the dataset's answer, and AnswerFacts the facts a
	// complete answer states.
	Reference   string   `json:"reference"`
	AnswerFacts []string `json:"answer_facts"`
	// ReferenceContextIDs are the documents that answer the question.
	ReferenceContextIDs []corpus.DocID `json:"reference_context_ids"`
	// RetrievedContextIDs are the documents get_context returned, in its
	// order, and RetrievedContexts each of its items as text.
	RetrievedContextIDs []corpus.DocID `json:"retrieved_context_ids"`
	RetrievedContexts   []string       `json:"retrieved_contexts"`
	// DirectContextIDs are the documents found by asking each app's search
	// for the question directly, directLimit from each: what get_context
	// would return if pear lost nothing on the way.
	DirectContextIDs []corpus.DocID `json:"direct_context_ids"`
	// IndexContextIDs are the best indexLimit documents of one index over
	// every app: what pear could return if it held the documents itself.
	IndexContextIDs []corpus.DocID `json:"index_context_ids"`
	// SuggestedTools are get_context's suggestions, best first, and
	// ReferenceTools the tools that search where the answer is.
	SuggestedTools []SuggestedTool `json:"suggested_tools"`
	ReferenceTools []string        `json:"reference_tools"`
	// Notes are get_context's notes: the sources it couldn't search.
	Notes []string `json:"notes,omitempty"`
	// MockCalls are the calls the mock app servers received.
	MockCalls  []mockmcp.Call `json:"mock_calls"`
	DurationMS int64          `json:"duration_ms"`
	// Error says why get_context failed, if it did.
	Error string `json:"error,omitempty"`
}

// contextOutput is the part of get_context's output a sample is built from
// (getContextOutput in internal/mcpserver).
type contextOutput struct {
	Items []struct {
		Server  string `json:"server"`
		Title   string `json:"title"`
		URL     string `json:"url"`
		Snippet string `json:"snippet"`
	} `json:"items"`
	SuggestedTools []SuggestedTool `json:"suggested_tools"`
	Notes          []string        `json:"notes"`
}

// yardsticks are searches of the same documents that don't go through pear,
// to compare get_context's results with.
type yardsticks struct {
	apps []mockmcp.App
	// perApp is each app's own index, as its mock server has.
	perApp map[mockmcp.App]*corpus.Index
	all    *corpus.Index
	match  corpus.Match
}

func newYardsticks(slice *corpus.Slice, apps []mockmcp.App, match corpus.Match) *yardsticks {
	y := &yardsticks{
		apps: apps, perApp: make(map[mockmcp.App]*corpus.Index),
		all: corpus.NewIndex(slice.Docs), match: match,
	}
	for _, app := range apps {
		y.perApp[app] = corpus.NewIndex(slice.OfSource(app.Source()))
	}
	return y
}

func (y *yardsticks) direct(query string) []corpus.DocID {
	out := []corpus.DocID{}
	for _, app := range y.apps {
		for _, hit := range y.perApp[app].Search(query, y.match, directLimit, nil) {
			out = append(out, hit.Doc.ID)
		}
	}
	return out
}

func (y *yardsticks) index(query string) []corpus.DocID {
	hits := y.all.Search(query, y.match, indexLimit, nil)
	out := make([]corpus.DocID, len(hits))
	for i, hit := range hits {
		out[i] = hit.Doc.ID
	}
	return out
}

// runQuestion asks pear's get_context the question and records what came
// back and what the mock servers were asked.
func runQuestion(
	ctx context.Context,
	session *mcp.ClientSession,
	stack *pearmcp.Stack,
	slice *corpus.Slice,
	y *yardsticks,
	q corpus.Question,
	maxTools int,
) Sample {
	s := Sample{
		QuestionID: q.ID, QuestionType: q.Type, Sources: q.Sources, UserInput: q.Text,
		Reference: q.GoldAnswer, AnswerFacts: q.AnswerFacts,
		ReferenceContextIDs: q.ExpectedDocs,
		RetrievedContextIDs: []corpus.DocID{}, RetrievedContexts: []string{},
		DirectContextIDs: y.direct(q.Text), IndexContextIDs: y.index(q.Text),
		SuggestedTools: []SuggestedTool{}, ReferenceTools: []string{},
	}
	for _, source := range q.Sources {
		if app := mockmcp.AppOf(source); app != "" {
			s.ReferenceTools = append(
				s.ReferenceTools,
				pearmcp.ServerName(app)+":"+app.SearchTool(),
			)
		}
	}

	stack.Log.Take() // drop anything left from before
	start := time.Now()
	out, err := getContext(ctx, session, q.Text, maxTools)
	s.DurationMS = time.Since(start).Milliseconds()
	s.MockCalls = stack.Log.Take()
	if err != nil {
		s.Error = err.Error()
		return s
	}
	for _, item := range out.Items {
		// A result names its document by URL: its own, or, for a tool
		// answering in text (Slack), the URLs in that text.
		for _, id := range slice.Mentioned(item.URL + "\n" + item.Snippet) {
			if !slices.Contains(s.RetrievedContextIDs, id) {
				s.RetrievedContextIDs = append(s.RetrievedContextIDs, id)
			}
		}
		s.RetrievedContexts = append(s.RetrievedContexts,
			strings.TrimSpace(item.Title+"\n"+item.Snippet))
	}
	s.SuggestedTools = append(s.SuggestedTools, out.SuggestedTools...)
	s.Notes = out.Notes
	return s
}

func getContext(
	ctx context.Context, session *mcp.ClientSession, query string, maxTools int,
) (*contextOutput, error) {
	res, err := session.CallTool(ctx, &mcp.CallToolParams{
		Name:      "get_context",
		Arguments: map[string]any{"query": query, "max_tools": maxTools},
	})
	if err != nil {
		return nil, fmt.Errorf("call get_context: %w", err)
	}
	if res.IsError {
		var text strings.Builder
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				text.WriteString(t.Text)
			}
		}
		return nil, fmt.Errorf("get_context failed: %s", text.String())
	}
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return nil, fmt.Errorf("read get_context output: %w", err)
	}
	var out contextOutput
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("read get_context output: %w", err)
	}
	return &out, nil
}
