package llmgateway_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/habitat-network/habitat/internal/llmgateway"
)

func TestNewModelFactoryBuildsEachProvider(t *testing.T) {
	factory := llmgateway.NewModelFactory(http.DefaultClient)

	for _, config := range []llmgateway.ModelConfig{
		{Provider: llmgateway.ProviderOpenAI, Model: "gpt-4o", APIKey: "k"},
		{
			Provider: llmgateway.ProviderOpenAI, Model: "m", APIKey: "k",
			BaseURL: "https://openrouter.ai/api/v1",
		},
		{Provider: llmgateway.ProviderAnthropic, Model: "claude", APIKey: "k"},
		{
			Provider: llmgateway.ProviderAnthropic, Model: "claude", APIKey: "k",
			BaseURL: "https://proxy.example.com",
		},
		{Provider: llmgateway.ProviderGoogle, Model: "gemini", APIKey: "k"},
		{Provider: llmgateway.ProviderOllama, Model: "llama3", BaseURL: "http://localhost:11434"},
	} {
		t.Run(string(config.Provider), func(t *testing.T) {
			model, err := factory(t.Context(), config)
			require.NoError(t, err)
			require.NoError(t, model.Close())
		})
	}
}

func TestNewModelFactoryRejectsUnknownProvider(t *testing.T) {
	_, err := llmgateway.NewModelFactory(http.DefaultClient)(
		t.Context(), llmgateway.ModelConfig{Provider: "nope", Model: "m"},
	)
	require.ErrorIs(t, err, llmgateway.ErrInvalidProvider)
}
