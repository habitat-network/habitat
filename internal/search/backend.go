package search

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/internal/fedsearch"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

const (
	// BackendKind is the fedsearch kind of the record search backend.
	BackendKind fedsearch.Kind = "records"
	// BackendID is the record search backend's id.
	BackendID fedsearch.BackendID = "records"
	// ResultKind is the fedsearch result kind of a record.
	ResultKind fedsearch.ResultKind = "record"
)

// RecordData is the fedsearch result data of a record.
type RecordData struct {
	Space      habitat_syntax.SpaceURI `json:"space"`
	Collection syntax.NSID             `json:"collection"`
	Value      map[string]any          `json:"value"`
}

// CallerSearcher runs a search as a caller. [Searcher] implements it.
type CallerSearcher interface {
	Search(ctx context.Context, caller syntax.DID, q Query) (Page, error)
}

// Resolver gives fedsearch one backend that searches the records a caller
// may read in an org.
type Resolver struct {
	searcher      CallerSearcher
	snippetLength int
}

var _ fedsearch.Resolver = (*Resolver)(nil)

// NewResolver returns a Resolver searching with searcher, with snippets of
// about snippetLength words (zero for [DefaultSnippetLength]).
func NewResolver(searcher CallerSearcher, snippetLength int) *Resolver {
	return &Resolver{searcher: searcher, snippetLength: snippetLength}
}

// Kind implements fedsearch.Resolver.
func (r *Resolver) Kind() fedsearch.Kind { return BackendKind }

// Backends implements fedsearch.Resolver.
func (r *Resolver) Backends(
	_ context.Context,
	caller, org syntax.DID,
) ([]fedsearch.Backend, error) {
	return []fedsearch.Backend{&backend{resolver: r, caller: caller, org: org}}, nil
}

// backend searches the records caller may read in org.
type backend struct {
	resolver *Resolver
	caller   syntax.DID
	org      syntax.DID
}

func (b *backend) ID() fedsearch.BackendID { return BackendID }
func (b *backend) Kind() fedsearch.Kind    { return BackendKind }
func (b *backend) Name() string            { return "Habitat records" }

// Search runs the record search as the caller; the index enforces who may
// read what.
func (b *backend) Search(ctx context.Context, q fedsearch.Query) ([]fedsearch.Result, error) {
	page, err := b.resolver.searcher.Search(ctx, b.caller, Query{
		Text: q.Text, Org: b.org, Limit: q.Limit, SnippetLength: b.resolver.snippetLength,
	})
	if err != nil {
		return nil, err
	}
	out := make([]fedsearch.Result, len(page.Matches))
	for i, m := range page.Matches {
		data, err := json.Marshal(RecordData{
			Space: m.Record.Space, Collection: m.Record.Collection, Value: m.Record.Value,
		})
		if err != nil {
			return nil, fmt.Errorf("encode record %s: %w", m.URI, err)
		}
		out[i] = fedsearch.Result{
			Kind:    ResultKind,
			ID:      m.URI.String(),
			Title:   string(m.Record.Collection),
			URL:     m.URI.String(),
			Snippet: unmark(m.Snippet),
			Data:    data,
		}
	}
	return out, nil
}

// unmark removes the <mark></mark> tags the index wraps matches in, since
// fedsearch snippets are plain text.
func unmark(s string) string {
	return strings.NewReplacer("<mark>", "", "</mark>", "").Replace(s)
}
