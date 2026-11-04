package llmgateway

import (
	"context"
	"fmt"
	"net/http"

	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/anthropic"
	"github.com/tmc/langchaingo/llms/googleai"
	"github.com/tmc/langchaingo/llms/ollama"
	"github.com/tmc/langchaingo/llms/openai"
)

// openAIBaseURL is OpenAI's own API, for openai connections without a base
// URL. It's passed explicitly so langchaingo never falls back to pear's own
// OPENAI_BASE_URL environment variable.
const openAIBaseURL = "https://api.openai.com/v1"

// Model is a connection's model, called through langchaingo's unified API.
// Some providers hold connections open, so the caller must Close it once done.
type Model interface {
	llms.Model
	Close() error
}

// ModelConfig is everything needed to call a connection's model.
type ModelConfig struct {
	Provider Provider
	Model    string
	// BaseURL is empty for the provider's default.
	BaseURL string
	APIKey  string
}

// ModelFactory builds the model a connection calls.
type ModelFactory func(ctx context.Context, config ModelConfig) (Model, error)

// noClose adapts an llms.Model that holds nothing open to [Model].
type noClose struct{ llms.Model }

func (noClose) Close() error { return nil }

// NewModelFactory returns a ModelFactory that calls providers through
// langchaingo over client. Every credential and URL comes from the
// connection: langchaingo would otherwise fall back to pear's own environment
// (OPENAI_API_KEY and the like), so each is passed explicitly.
func NewModelFactory(client *http.Client) ModelFactory {
	return func(ctx context.Context, config ModelConfig) (Model, error) {
		switch config.Provider {
		case ProviderOpenAI:
			baseURL := config.BaseURL
			if baseURL == "" {
				baseURL = openAIBaseURL
			}
			m, err := openai.New(
				openai.WithToken(config.APIKey),
				openai.WithModel(config.Model),
				openai.WithBaseURL(baseURL),
				openai.WithHTTPClient(client),
			)
			if err != nil {
				return nil, fmt.Errorf("new openai model: %w", err)
			}
			return noClose{m}, nil
		case ProviderAnthropic:
			opts := []anthropic.Option{
				anthropic.WithToken(config.APIKey),
				anthropic.WithModel(config.Model),
				anthropic.WithHTTPClient(client),
			}
			if config.BaseURL != "" {
				opts = append(opts, anthropic.WithBaseURL(config.BaseURL))
			}
			m, err := anthropic.New(opts...)
			if err != nil {
				return nil, fmt.Errorf("new anthropic model: %w", err)
			}
			return noClose{m}, nil
		case ProviderGoogle:
			// googleai has no base URL option: it always calls Google's API.
			m, err := googleai.New(
				ctx,
				googleai.WithAPIKey(config.APIKey),
				googleai.WithDefaultModel(config.Model),
				googleai.WithHTTPClient(client),
			)
			if err != nil {
				return nil, fmt.Errorf("new google model: %w", err)
			}
			return m, nil
		case ProviderOllama:
			m, err := ollama.New(
				ollama.WithModel(config.Model),
				ollama.WithServerURL(config.BaseURL),
				ollama.WithHTTPClient(client),
			)
			if err != nil {
				return nil, fmt.Errorf("new ollama model: %w", err)
			}
			return noClose{m}, nil
		default:
			return nil, fmt.Errorf("%w: %q", ErrInvalidProvider, config.Provider)
		}
	}
}
