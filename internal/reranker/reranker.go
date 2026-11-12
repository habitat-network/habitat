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
	// Rerank returns the indexes into documents of the (at most) topN most
	// relevant documents, most relevant first. Each index appears at most
	// once. A topN of zero or less means all documents.
	Rerank(ctx context.Context, query string, documents []string, topN int) ([]int, error)
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
) ([]int, error) {
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
	order := make([]int, 0, len(resp.Results))
	seen := make(map[int]bool, len(resp.Results))
	for _, r := range resp.Results {
		if r.Index < 0 || r.Index >= len(documents) || seen[r.Index] {
			return nil, fmt.Errorf("cohere rerank: unexpected result index %d", r.Index)
		}
		seen[r.Index] = true
		order = append(order, r.Index)
	}
	return order, nil
}
