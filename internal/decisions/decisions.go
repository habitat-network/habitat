// Package decisions calls an OpenAI-compatible Decisions API
// (https://developers.openai.com/api/docs/guides/decisions), which answers
// typed questions about some input with probabilities instead of generating
// text.
//
// Unlike internal/llmgateway, whose models are connections an org configures,
// the decisions endpoint is configured once for the whole deployment, so New
// takes its base URL, key and model directly (e.g. an Azure-hosted model).
package decisions

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
)

// ErrRefused is returned when the model declines to answer a question.
var ErrRefused = errors.New("decision refused")

// Client asks questions of a decisions endpoint.
type Client struct {
	decisions openai.DecisionService
	model     string
}

// New returns a Client that calls the decisions endpoint under baseURL (for
// Azure, https://<resource>.openai.azure.com/openai/v1) with apiKey as a
// bearer token. model is the model name, or for Azure the deployment name.
// Every value is passed explicitly so the SDK never falls back to pear's own
// OPENAI_* environment variables.
func New(baseURL, apiKey, model string, httpClient *http.Client) *Client {
	c := openai.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(apiKey),
		option.WithHTTPClient(httpClient),
	)
	return &Client{decisions: c.Decisions, model: model}
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
	questions := make([]openai.DecisionNewParamsQuestionUnion, len(predicates))
	for i, p := range predicates {
		questions[i] = openai.DecisionNewParamsQuestionUnion{
			OfPredicate: &openai.DecisionNewParamsQuestionPredicate{
				Name: param.NewOpt(p.Name),
				Instructions: openai.DecisionNewParamsQuestionPredicateInstructionsUnion{
					OfString: param.NewOpt(p.Instructions),
				},
			},
		}
	}
	answers, err := c.ask(ctx, input, questions)
	if err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(answers))
	for i, a := range answers {
		if a.Type == "refusal" {
			return nil, fmt.Errorf("%w: %q", ErrRefused, predicates[i].Name)
		}
		out[a.Name] = a.Probability
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
	choices := make([]openai.DecisionNewParamsQuestionChoiceChoice, len(options))
	for i, o := range options {
		choices[i] = openai.DecisionNewParamsQuestionChoiceChoice{
			Value: openai.DecisionNewParamsQuestionChoiceChoiceValueUnion{
				OfString: param.NewOpt(o.Value),
			},
		}
		if o.Description != "" {
			choices[i].Description = param.NewOpt(o.Description)
		}
	}
	answers, err := c.ask(ctx, input, []openai.DecisionNewParamsQuestionUnion{{
		OfChoice: &openai.DecisionNewParamsQuestionChoice{
			Choices: choices,
			Instructions: openai.DecisionNewParamsQuestionChoiceInstructionsUnion{
				OfString: param.NewOpt(instructions),
			},
		},
	}})
	if err != nil {
		return Choice{}, err
	}
	if answers[0].Type == "refusal" {
		return Choice{}, ErrRefused
	}
	choice := answers[0].AsChoice()
	probabilities := make(map[string]float64, len(choice.Probabilities))
	for _, p := range choice.Probabilities {
		probabilities[p.Value.OfString] = p.Probability
	}
	return Choice{
		Value:         choice.Choice.OfString,
		Confidence:    choice.Confidence,
		Probabilities: probabilities,
	}, nil
}

// ask sends the questions and checks one answer came back for each.
func (c *Client) ask(
	ctx context.Context,
	input string,
	questions []openai.DecisionNewParamsQuestionUnion,
) ([]openai.DecisionAnswerUnion, error) {
	res, err := c.decisions.New(ctx, openai.DecisionNewParams{
		Model:     c.model,
		Input:     openai.DecisionNewParamsInputUnion{OfString: param.NewOpt(input)},
		Questions: questions,
	})
	if err != nil {
		return nil, fmt.Errorf("decisions request: %w", err)
	}
	if len(res.Answers) != len(questions) {
		return nil, fmt.Errorf(
			"decisions returned %d answers for %d questions", len(res.Answers), len(questions),
		)
	}
	return res.Answers, nil
}
