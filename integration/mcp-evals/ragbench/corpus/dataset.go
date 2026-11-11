// Package corpus loads EnterpriseRAG-Bench
// (https://github.com/onyx-dot-app/EnterpriseRAG-Bench), a synthetic company's
// documents across Slack, Google Drive, GitHub and other apps with questions
// whose answering documents are known, and cuts it down to a slice small
// enough to hold and search in memory.
//
// The dataset is never committed here: it asks to be kept out of training
// corpora, so runs read a local checkout (see ../README.md).
package corpus

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// DocID is a document's id in the dataset, like "dsid_ae06...".
type DocID string

// Source is the app a document lives in, named as the dataset names it.
type Source string

const (
	SourceSlack       Source = "slack"
	SourceGoogleDrive Source = "google_drive"
	SourceGitHub      Source = "github"
)

// QuestionID is a question's id in the dataset, like "qst_0001".
type QuestionID string

// QuestionType is the dataset's category of a question, like "basic" or
// "semantic".
type QuestionType string

// Question is one line of the dataset's questions.jsonl.
type Question struct {
	ID   QuestionID   `json:"question_id"`
	Type QuestionType `json:"question_type"`
	// Sources are the apps holding ExpectedDocs.
	Sources []Source `json:"source_types"`
	Text    string   `json:"question"`
	// ExpectedDocs are the documents that answer the question; none for
	// questions the corpus can't answer.
	ExpectedDocs []DocID  `json:"expected_doc_ids"`
	GoldAnswer   string   `json:"gold_answer"`
	AnswerFacts  []string `json:"answer_facts"`
}

// ErrNoDataset is returned when a directory doesn't hold the dataset.
var ErrNoDataset = errors.New("not an EnterpriseRAG-Bench checkout")

// Dataset is a checkout of the dataset on disk. Documents are read only when
// a slice asks for them.
type Dataset struct {
	dir string
	// paths maps every document to its file, relative to the sources
	// directory, like "slack/eng/1719876543-quick-check.json".
	paths     map[DocID]string
	questions []Question
}

// Open reads the index and questions of the dataset checked out at dir (the
// repository's root).
func Open(dir string) (*Dataset, error) {
	raw, err := os.ReadFile(filepath.Join(dir, "generated_data", "uuid_index.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("%w: %s has no generated_data/uuid_index.json", ErrNoDataset, dir)
	}
	if err != nil {
		return nil, err
	}
	d := &Dataset{dir: dir}
	if err := json.Unmarshal(raw, &d.paths); err != nil {
		return nil, fmt.Errorf("read uuid_index.json: %w", err)
	}
	if d.questions, err = readQuestions(filepath.Join(dir, "questions.jsonl")); err != nil {
		return nil, err
	}
	return d, nil
}

func readQuestions(file string) ([]Question, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out []Question
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == "" {
			continue
		}
		var q Question
		if err := json.Unmarshal(scanner.Bytes(), &q); err != nil {
			return nil, fmt.Errorf("read %s: %w", file, err)
		}
		out = append(out, q)
	}
	return out, scanner.Err()
}

// Questions returns every question, in the dataset's order.
func (d *Dataset) Questions() []Question { return slices.Clone(d.questions) }

// sourceOf is the app the document at p (a path under the sources directory)
// lives in: its first path element.
func sourceOf(p string) Source {
	first, _, _ := strings.Cut(p, "/")
	return Source(first)
}

// load reads the document id from disk.
func (d *Dataset) load(id DocID) (*Doc, error) {
	p, ok := d.paths[id]
	if !ok {
		return nil, fmt.Errorf("document %s isn't in the dataset's index", id)
	}
	raw, err := os.ReadFile(
		filepath.Join(d.dir, "generated_data", "sources", filepath.FromSlash(p)),
	)
	if err != nil {
		return nil, fmt.Errorf("read document %s (is its source checked out?): %w", id, err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("read document %s at %s: %w", id, p, err)
	}
	return newDoc(id, p, fields), nil
}

// dirOf is the directory of the document at p: a Slack channel, a GitHub
// repository or a Drive folder.
func dirOf(p string) string { return path.Dir(p) }
