package corpus

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// Doc is one document of the dataset: a Slack thread, a GitHub pull request
// or a Drive file.
type Doc struct {
	ID     DocID
	Source Source
	// Path is the document's file, relative to the dataset's sources
	// directory.
	Path string
	// Title and Text are the fields the dataset says are the document's
	// title and content (its title_field_name and content_field_names).
	Title string
	Text  string
	// Fields is the whole document as the dataset has it, including the
	// app-specific metadata (a PR's repo and number, a thread's channel).
	Fields map[string]any
	// URL is where the document would live in its app. Slices make it unique,
	// so it identifies the document in whatever a search returns.
	URL string
}

// Field returns the document's field name as text: a string as is, a list one
// item per line, anything else as JSON. A missing field is "".
func (d *Doc) Field(name string) string { return fieldText(d.Fields[name]) }

func fieldText(v any) string {
	switch v := v.(type) {
	case nil:
		return ""
	case string:
		return v
	case []any:
		parts := make([]string, 0, len(v))
		for _, item := range v {
			parts = append(parts, fieldText(item))
		}
		return strings.Join(parts, "\n")
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(raw)
	}
}

// fieldList returns the field name's items as text, for a list field, or the
// field itself as one item.
func (d *Doc) fieldList(name string) []string {
	list, ok := d.Fields[name].([]any)
	if !ok {
		if s := d.Field(name); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		out = append(out, fieldText(item))
	}
	return out
}

func newDoc(id DocID, p string, fields map[string]any) *Doc {
	d := &Doc{ID: id, Source: sourceOf(p), Path: p, Fields: fields}
	d.Title = d.Field(d.Field("title_field_name"))
	var content []string
	for _, name := range d.fieldList("content_field_names") {
		if s := d.Field(name); s != "" {
			content = append(content, s)
		}
	}
	d.Text = strings.Join(content, "\n\n")
	d.URL = d.naturalURL()
	return d
}

// Workspace names the fictional company's accounts in each app.
const (
	slackWorkspace = "redwood-inference"
	githubOwner    = "redwood-inference"
)

// naturalURL is the URL the document would have in its app, built from its
// own metadata. Two documents can share one (the dataset holds
// near-duplicates), which Slice resolves.
func (d *Doc) naturalURL() string {
	hex := strings.TrimPrefix(string(d.ID), "dsid_")
	switch d.Source {
	case SourceSlack:
		return fmt.Sprintf("https://%s.slack.com/archives/%s/p%s",
			slackWorkspace, urlSegment(d.Field("channel")), urlSegment(d.Field("thread_ts")))
	case SourceGitHub:
		return fmt.Sprintf("https://github.com/%s/%s/pull/%s",
			githubOwner, urlSegment(d.Repo()), urlSegment(d.Field("pr_number")))
	case SourceGoogleDrive:
		return fmt.Sprintf("https://drive.google.com/file/d/%s/view", hex)
	default:
		return fmt.Sprintf("https://%s.example/%s", d.Source, hex)
	}
}

// Repo is the GitHub repository a pull request belongs to.
func (d *Doc) Repo() string {
	if repo := d.Field("repo"); repo != "" {
		return path.Base(repo)
	}
	return "redwood"
}

// urlSegment makes s safe as one path segment of a URL.
func urlSegment(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "unknown"
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.':
			return r
		default:
			return '-'
		}
	}, s)
}
