// Package reranker reorders candidate documents by their relevance to a
// query, using a cross-encoder reranking model such as Cohere's Rerank.
//
// Retrieval (full-text search, federated search) is cheap but coarse; a
// reranker reads the query and each candidate together and scores them far
// more accurately, so it is run over the few candidates retrieval returned.
package reranker

import (
	"context"
	"fmt"
	"net/http"

	cohere "github.com/cohere-ai/cohere-go/v2"
	cohereclient "github.com/cohere-ai/cohere-go/v2/client"
	"github.com/cohere-ai/cohere-go/v2/option"
)

// Reranker scores documents against a query.
type Reranker interface {
	// Rerank returns the (at most) topN most relevant documents, most
	// relevant first, each as its index into documents with its relevance
	// score. Each index appears at most once. A topN of zero or less means all
	// documents.
	//
	// A score depends only on the query and the document, not on the other
	// documents in the call, so scores from separate calls with the same
	// query can be compared.
	Rerank(ctx context.Context, query string, documents []string, topN int) ([]Scored, error)
}

// Scored is one document's place in a reranking.
type Scored struct {
	// Index is the document's index in the documents passed to Rerank.
	Index int
	// Score is its relevance to the query, higher is more relevant. For
	// Cohere it is between 0 and 1.
	Score float64
}

// Cohere reranks with the Cohere Rerank v2 API, either Cohere's own or a
// deployment of it such as Azure AI Foundry's.
type Cohere struct {
	client *cohereclient.Client
	model  string
}

// NewCohere returns a Cohere reranker that posts to baseURL + "/v2/rerank"
// with apiKey as a bearer token. baseURL is the deployment's origin, such as
// "https://<name>.<region>.models.ai.azure.com" for Azure, or empty for
// Cohere's own API. model is the model name, such as "rerank-v3.5", or the
// Azure deployment name. httpClient may be nil for the default.
func NewCohere(baseURL, apiKey, model string, httpClient *http.Client) *Cohere {
	opts := []option.RequestOption{cohereclient.WithToken(apiKey)}
	if baseURL != "" {
		opts = append(opts, cohereclient.WithBaseURL(baseURL))
	}
	if httpClient != nil {
		opts = append(opts, cohereclient.WithHTTPClient(httpClient))
	}
	return &Cohere{client: cohereclient.NewClient(opts...), model: model}
}

// Rerank implements Reranker.
func (c *Cohere) Rerank(
	ctx context.Context,
	query string,
	documents []string,
	topN int,
) ([]Scored, error) {
	if len(documents) == 0 {
		return nil, nil
	}
	req := &cohere.V2RerankRequest{Model: c.model, Query: query, Documents: documents}
	if topN > 0 {
		req.TopN = &topN
	}
	resp, err := c.client.V2.Rerank(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("cohere rerank: %w", err)
	}
	order := make([]Scored, 0, len(resp.Results))
	seen := make(map[int]bool, len(resp.Results))
	for _, r := range resp.Results {
		if r.Index < 0 || r.Index >= len(documents) || seen[r.Index] {
			return nil, fmt.Errorf("cohere rerank: unexpected result index %d", r.Index)
		}
		seen[r.Index] = true
		order = append(order, Scored{Index: r.Index, Score: r.RelevanceScore})
	}
	return order, nil
}
