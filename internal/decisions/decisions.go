// Package decisions calls a Jev-format decision API, which answers typed
// questions about some input with probabilities instead of generating text.
//
// The wire format is TypeSafe's System One / Jev (POST .../systemone with a
// state and named questions), served by TypeSafe, Azure Foundry's
// Microsoft-Decision-1 and Ollama's decision models; it is not OpenAI's
// /v1/decisions format.
//
// Unlike internal/llmgateway, whose models are connections an org configures,
// the decisions endpoint is configured once for the whole deployment, so New
// takes its URL, key and model directly (e.g. an Azure-hosted model).
package decisions

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/mattn/go-jev"
)

// ErrRefused is returned when the model declines to answer a question. The
// Jev format has no refusal answer, so it's returned when a question comes
// back without an answer.
var ErrRefused = errors.New("decision refused")

// Client asks questions of a decisions endpoint.
type Client struct {
	jev *jev.Client
}

// New returns a Client that posts to the decisions endpoint at url, exactly as
// given (for Azure, <resource endpoint>/providers/microsoft/v1/systemone; for
// Ollama, http://localhost:11434/v1/systemone), with apiKey as a bearer token.
// model is the model name, or for Azure the deployment name. Every value is
// passed explicitly: go-jev reads no environment variables.
func New(url, apiKey, model string, httpClient *http.Client) *Client {
	return &Client{jev: jev.NewClient(
		jev.WithURL(url),
		jev.WithAPIKey(apiKey),
		jev.WithModel(model),
		jev.WithHTTPClient(httpClient),
	)}
}

// Predicate is a yes/no question about the input.
type Predicate struct {
	// Name identifies the question's answer and must be unique in a call.
	Name         string
	Instructions string
}

// Predicates asks every predicate about input in one request and returns the
// probability, from 0 to 1, that each one's condition holds, by name.
func (c *Client) Predicates(
	ctx context.Context,
	input string,
	predicates []Predicate,
) (map[string]float64, error) {
	questions := make(map[string]jev.Question, len(predicates))
	for _, p := range predicates {
		questions[p.Name] = jev.Question{Type: "noul", Instructions: p.Instructions}
	}
	resp, err := c.jev.Evaluate(ctx, input, questions)
	if err != nil {
		return nil, fmt.Errorf("decisions request: %w", err)
	}
	out := make(map[string]float64, len(predicates))
	for _, p := range predicates {
		answer := resp.Answers[p.Name]
		if answer == nil {
			return nil, fmt.Errorf("%w: %q", ErrRefused, p.Name)
		}
		out[p.Name] = answer.Noul
	}
	return out, nil
}

// Option is one of the values Choose picks between.
type Option struct {
	Value       string
	Description string
}

// Choice is the answer to a choice question.
type Choice struct {
	// Value is the most likely option's value.
	Value string
	// Confidence is the model's confidence in Value, from 0 to 1.
	Confidence float64
	// Probabilities is each option's probability, by value.
	Probabilities map[string]float64
}

// Choose asks which of options best fits input under instructions.
func (c *Client) Choose(
	ctx context.Context,
	input string,
	instructions string,
	options []Option,
) (Choice, error) {
	criteria := make(jev.Options, len(options))
	for i, o := range options {
		criteria[i] = jev.Option{Name: o.Value}
		// A nil description is sent as null; an empty one would be sent as "".
		if o.Description != "" {
			criteria[i].Desc = o.Description
		}
	}
	answer, err := c.jev.Ask(ctx, input, jev.Question{
		Type:         "choice",
		Instructions: instructions,
		Criteria:     criteria,
	})
	if err != nil {
		return Choice{}, fmt.Errorf("decisions request: %w", err)
	}
	return Choice{
		Value:         answer.Choice,
		Confidence:    answer.Confidence,
		Probabilities: answer.Probabilities,
	}, nil
}
