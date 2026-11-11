// Package benchtest writes a tiny dataset in EnterpriseRAG-Bench's layout,
// for tests.
package benchtest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Doc is a document to write: its id, its path under the dataset's sources
// directory, and its fields.
type Doc struct {
	ID     string
	Path   string
	Fields map[string]any
}

// Slack returns a Slack thread.
func Slack(id, channel, ts, messages string) Doc {
	return Doc{ID: id, Path: "slack/" + channel + "/" + ts + ".json", Fields: map[string]any{
		"channel": channel, "thread_ts": ts, "participants": []string{"ava", "diego"},
		"messages": messages, "title_field_name": "channel",
		"content_field_names": []string{"messages"},
	}}
}

// PullRequest returns a GitHub pull request.
func PullRequest(id, repo, number, title, description string) Doc {
	return Doc{ID: id, Path: "github/" + repo + "/pr-" + number + ".json", Fields: map[string]any{
		"repo": repo, "pr_number": number, "title": title, "author": "Priya Menon",
		"state": "merged", "labels": []string{"runtime"}, "description": description,
		"title_field_name": "title", "content_field_names": []string{"description"},
	}}
}

// DriveFile returns a Google Drive document.
func DriveFile(id, folder, title, content string) Doc {
	return Doc{
		ID: id, Path: "google_drive/shared_drives/" + folder + "/" + id + ".json",
		Fields: map[string]any{
			"title": title, "owner": "Maya Chen", "doc_type": "doc", "content": content,
			"title_field_name": "title", "content_field_names": []string{"content"},
		},
	}
}

// Question is a question to write: its id, the app holding its answer, its
// text and the documents answering it.
type Question struct {
	ID, Type, Source, Text string
	Docs                   []string
}

// Write writes a dataset of docs and questions to a new directory and
// returns it.
func Write(t *testing.T, docs []Doc, questions []Question) string {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "generated_data"), 0o755))
	index := make(map[string]string, len(docs))
	for _, doc := range docs {
		index[doc.ID] = doc.Path
		file := filepath.Join(dir, "generated_data", "sources", filepath.FromSlash(doc.Path))
		require.NoError(t, os.MkdirAll(filepath.Dir(file), 0o755))
		raw, err := json.Marshal(doc.Fields)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(file, raw, 0o644))
	}
	raw, err := json.Marshal(index)
	require.NoError(t, err)
	require.NoError(t,
		os.WriteFile(filepath.Join(dir, "generated_data", "uuid_index.json"), raw, 0o644))

	var lines []byte
	for _, q := range questions {
		sources := []string{}
		if q.Source != "" {
			sources = append(sources, q.Source)
		}
		raw, err := json.Marshal(map[string]any{
			"question_id": q.ID, "question_type": q.Type, "source_types": sources,
			"question": q.Text, "expected_doc_ids": q.Docs,
			"gold_answer": "answer to " + q.ID, "answer_facts": []string{"fact of " + q.ID},
		})
		require.NoError(t, err)
		lines = append(append(lines, raw...), '\n')
	}
	require.NoError(t, os.WriteFile(filepath.Join(dir, "questions.jsonl"), lines, 0o644))
	return dir
}

// Redwood is a small dataset with one answerable question per app, one
// question whose answer isn't in the corpus, and distractor documents.
func Redwood(t *testing.T) string {
	t.Helper()
	docs := []Doc{
		Slack(
			"dsid_a1",
			"incidents",
			"1700000001",
			"ava: the kestrel cache outage was caused by an expired certificate\ndiego: renewing now",
		),
		Slack(
			"dsid_a2",
			"incidents",
			"1700000002",
			"ava: paging the on-call for the billing queue",
		),
		Slack("dsid_a3", "random", "1700000003", "diego: lunch is tacos today"),
		PullRequest("dsid_b1", "redwood", "101", "Add multipart upload limits",
			"Sets max_file_size to 10 MiB for multipart uploads."),
		PullRequest("dsid_b2", "redwood", "102", "Fix scheduler retry backoff",
			"Retries now back off exponentially."),
		PullRequest("dsid_b3", "installer", "7", "Bump base image", "Moves to a newer base image."),
		DriveFile("dsid_c1", "finance", "Headcount ramp model",
			"The runway sensitivity model assumes hiring four engineers a quarter."),
		DriveFile(
			"dsid_c2",
			"finance",
			"Travel policy",
			"Economy class for flights under six hours.",
		),
		DriveFile("dsid_c3", "product", "Roadmap notes", "Quarterly roadmap themes and owners."),
	}
	return Write(t, docs, []Question{
		{ID: "qst_0001", Type: "basic", Source: "slack", Docs: []string{"dsid_a1"},
			Text: "What caused the kestrel cache outage?"},
		{ID: "qst_0002", Type: "basic", Source: "github", Docs: []string{"dsid_b1"},
			Text: "What is the multipart upload file size limit?"},
		{ID: "qst_0003", Type: "semantic", Source: "google_drive", Docs: []string{"dsid_c1"},
			Text: "How many engineers does the runway model assume we hire each quarter?"},
		{ID: "qst_0004", Type: "info_not_found", Text: "What is the office wifi password?"},
	})
}
