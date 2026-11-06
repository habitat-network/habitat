package llmgateway_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms"

	"github.com/habitat-network/habitat/internal/llmgateway"
)

func TestChatCompletionRequestTranslation(t *testing.T) {
	var req llmgateway.ChatCompletionRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model": "gpt",
		"stop": "END",
		"temperature": 0.2,
		"max_tokens": 10,
		"max_completion_tokens": 20,
		"tools": [{"type":"function","function":{"name":"f","parameters":{"type":"object"}}}],
		"tool_choice": {"type":"function","function":{"name":"f"}},
		"response_format": {"type":"json_object"},
		"messages": [
			{"role":"system","content":"be brief"},
			{"role":"user","content":[
				{"type":"text","text":"look"},
				{"type":"image_url","image_url":{"url":"https://x/y.png"}}
			]},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"c1","type":"function","function":{"name":"f","arguments":"{}"}}
			]},
			{"role":"tool","tool_call_id":"c1","content":"ok"}
		]
	}`), &req))

	msgs, err := req.LangchainMessages()
	require.NoError(t, err)
	require.Len(t, msgs, 4)
	require.Equal(t, llms.ChatMessageTypeSystem, msgs[0].Role)
	require.Len(t, msgs[1].Parts, 2)
	require.IsType(t, llms.ImageURLContent{}, msgs[1].Parts[1])
	call, isCall := msgs[2].Parts[0].(llms.ToolCall)
	require.True(t, isCall)
	require.Equal(t, "f", call.FunctionCall.Name)
	require.Equal(t, llms.ChatMessageTypeTool, msgs[3].Role)

	// Stop, sampling, token limits (max_completion_tokens wins), tools,
	// tool_choice and JSON mode all become call options.
	opts, err := req.CallOptions()
	require.NoError(t, err)
	var got llms.CallOptions
	for _, opt := range opts {
		opt(&got)
	}
	require.Equal(t, []string{"END"}, got.StopWords)
	require.InDelta(t, 0.2, got.Temperature, 1e-9)
	require.Equal(t, 20, got.MaxTokens)
	require.Len(t, got.Tools, 1)
	require.True(t, got.JSONMode)
	require.Equal(t, llms.ToolChoice{
		Type: "function", Function: &llms.FunctionReference{Name: "f"},
	}, got.ToolChoice)
}

func TestChatCompletionRequestRejectsUnsupported(t *testing.T) {
	var req llmgateway.ChatCompletionRequest
	_, err := req.LangchainMessages()
	require.ErrorIs(t, err, llmgateway.ErrInvalidChatRequest)

	require.NoError(
		t,
		json.Unmarshal([]byte(`{"messages":[{"role":"wizard","content":"x"}]}`), &req),
	)
	_, err = req.LangchainMessages()
	require.ErrorIs(t, err, llmgateway.ErrInvalidChatRequest)

	req = llmgateway.ChatCompletionRequest{Tools: []llmgateway.Tool{{Type: "retrieval"}}}
	_, err = req.CallOptions()
	require.ErrorIs(t, err, llmgateway.ErrInvalidChatRequest)
}

func TestChatCompletionForNormalizesProviderResponse(t *testing.T) {
	completion := llmgateway.ChatCompletionFor(
		&llms.ContentResponse{Choices: []*llms.ContentChoice{{
			Content:        "hello",
			StopReason:     "max_tokens",
			GenerationInfo: map[string]any{"InputTokens": 3, "OutputTokens": 4},
		}}},
		"id1",
		5,
		"gpt",
		nil,
	)
	require.Equal(t, "chat.completion", completion.Object)
	require.Equal(t, "hello", *completion.Choices[0].Message.Content)
	require.Equal(t, "length", completion.Choices[0].FinishReason)
	require.Equal(
		t,
		&llmgateway.Usage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7},
		completion.Usage,
	)

	// A tool-only reply has null content and finishes with tool_calls.
	completion = llmgateway.ChatCompletionFor(&llms.ContentResponse{Choices: []*llms.ContentChoice{{
		ToolCalls: []llms.ToolCall{{
			ID: "c", Type: "function", FunctionCall: &llms.FunctionCall{Name: "f", Arguments: "{}"},
		}},
	}}}, "id2", 5, "gpt", nil)
	require.Nil(t, completion.Choices[0].Message.Content)
	require.Equal(t, "tool_calls", completion.Choices[0].FinishReason)
	require.Equal(t, "f", completion.Choices[0].Message.ToolCalls[0].Function.Name)
	require.Nil(t, completion.Usage)
}

// Tool names OpenAI and Anthropic reject, like the "server:tool" names of
// pear's MCP endpoint, are sent to the provider under an alias they accept,
// consistently across the conversation, and mapped back in the response.
func TestToolNamesAreAliasedForProviders(t *testing.T) {
	long := "server:" + strings.Repeat("x", 80)
	var req llmgateway.ChatCompletionRequest
	require.NoError(t, json.Unmarshal([]byte(`{
		"model": "gpt",
		"tools": [
			{"type":"function","function":{"name":"github:get_me"}},
			{"type":"function","function":{"name":"github_get_me"}},
			{"type":"function","function":{"name":"`+long+`"}},
			{"type":"function","function":{"name":"search_records"}}
		],
		"tool_choice": {"type":"function","function":{"name":"github:get_me"}},
		"messages": [
			{"role":"user","content":"who am i"},
			{"role":"assistant","content":null,"tool_calls":[
				{"id":"c1","type":"function","function":{"name":"github:get_me","arguments":"{}"}}
			]},
			{"role":"tool","tool_call_id":"c1","name":"github:get_me","content":"octocat"}
		]
	}`), &req))

	opts, err := req.CallOptions()
	require.NoError(t, err)
	var got llms.CallOptions
	for _, opt := range opts {
		opt(&got)
	}
	valid := regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	sent := make([]string, len(got.Tools))
	for i, tool := range got.Tools {
		sent[i] = tool.Function.Name
		require.Regexp(t, valid, sent[i])
	}
	alias := sent[0]
	require.NotEqual(t, "github_get_me", alias, "aliases don't collide with real names")
	require.Equal(t, "github_get_me", sent[1])
	require.Equal(t, "search_records", sent[3])
	require.NotEqual(t, sent[0], sent[2])
	require.Equal(t, alias, got.ToolChoice.(llms.ToolChoice).Function.Name)

	msgs, err := req.LangchainMessages()
	require.NoError(t, err)
	require.Equal(t, alias, msgs[1].Parts[0].(llms.ToolCall).FunctionCall.Name)
	require.Equal(t, alias, msgs[2].Parts[0].(llms.ToolCallResponse).Name)

	completion := llmgateway.ChatCompletionFor(&llms.ContentResponse{Choices: []*llms.ContentChoice{{
		ToolCalls: []llms.ToolCall{
			{ID: "c2", Type: "function", FunctionCall: &llms.FunctionCall{Name: alias}},
			{ID: "c3", Type: "function", FunctionCall: &llms.FunctionCall{Name: sent[2]}},
			{ID: "c4", Type: "function", FunctionCall: &llms.FunctionCall{Name: "made_up"}},
		},
	}}}, "id", 1, "gpt", req.ToolNames())
	calls := completion.Choices[0].Message.ToolCalls
	require.Equal(t, "github:get_me", calls[0].Function.Name)
	require.Equal(t, long, calls[1].Function.Name)
	require.Equal(t, "made_up", calls[2].Function.Name)
}
