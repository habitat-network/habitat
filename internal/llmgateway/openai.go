package llmgateway

// This file translates between the OpenAI Chat Completions wire format
// (https://platform.openai.com/docs/api-reference/chat) and langchaingo's
// unified API. It covers the fields every provider can honor: text and image
// content, sampling parameters, tools and JSON mode. OpenRouter and most other
// gateways speak the same format, so any OpenAI client works against pear.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/tmc/langchaingo/llms"
)

// ErrInvalidChatRequest is returned for a chat completion request pear can't
// translate.
var ErrInvalidChatRequest = errors.New("invalid chat completion request")

// ChatCompletionRequest is the body of POST /chat/completions.
type ChatCompletionRequest struct {
	// Model is the connection's name.
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`

	Stream        bool           `json:"stream,omitempty"`
	StreamOptions *StreamOptions `json:"stream_options,omitempty"`

	Temperature         *float64 `json:"temperature,omitempty"`
	TopP                *float64 `json:"top_p,omitempty"`
	MaxTokens           *int     `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int     `json:"max_completion_tokens,omitempty"`
	Stop                Stop     `json:"stop,omitempty"`
	N                   *int     `json:"n,omitempty"`
	Seed                *int     `json:"seed,omitempty"`
	FrequencyPenalty    *float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty     *float64 `json:"presence_penalty,omitempty"`

	Tools []Tool `json:"tools,omitempty"`
	// ToolChoice is "none", "auto", "required", or a {"type": "function",
	// "function": {"name": ...}} object.
	ToolChoice     json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// StreamOptions configures a streamed response.
type StreamOptions struct {
	// IncludeUsage adds a final chunk carrying token usage.
	IncludeUsage bool `json:"include_usage,omitempty"`
}

// Stop is the stop parameter, which OpenAI accepts as a string or an array.
type Stop []string

// UnmarshalJSON accepts a string or an array of strings.
func (s *Stop) UnmarshalJSON(data []byte) error {
	var one string
	if err := json.Unmarshal(data, &one); err == nil {
		*s = Stop{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(data, &many); err != nil {
		return fmt.Errorf("stop must be a string or an array of strings: %w", err)
	}
	*s = many
	return nil
}

// ResponseFormat is the response_format parameter.
type ResponseFormat struct {
	// Type is "text", "json_object" or "json_schema".
	Type string `json:"type"`
}

// ChatMessage is a message in a chat completion request.
type ChatMessage struct {
	// Role is "system", "developer", "user", "assistant" or "tool".
	Role       string         `json:"role"`
	Content    MessageContent `json:"content"`
	Name       string         `json:"name,omitempty"`
	ToolCalls  []ToolCall     `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

// MessageContent is a request message's content, which OpenAI accepts as a
// string or an array of content parts.
type MessageContent []ContentPart

// ContentPart is one part of a message's content.
type ContentPart struct {
	// Type is "text" or "image_url".
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL is an image_url content part's image.
type ImageURL struct {
	// URL is an http(s) or data: URL.
	URL    string `json:"url"`
	Detail string `json:"detail,omitempty"`
}

// UnmarshalJSON accepts a string, an array of content parts, or null.
func (c *MessageContent) UnmarshalJSON(data []byte) error {
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		*c = nil
		return nil
	}
	var text string
	if err := json.Unmarshal(data, &text); err == nil {
		*c = MessageContent{{Type: "text", Text: text}}
		return nil
	}
	var parts []ContentPart
	if err := json.Unmarshal(data, &parts); err != nil {
		return fmt.Errorf("content must be a string or an array of content parts: %w", err)
	}
	*c = parts
	return nil
}

// text joins the content's text parts.
func (c MessageContent) text() string {
	var b strings.Builder
	for _, part := range c {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return b.String()
}

// Tool is a tool the model may call.
type Tool struct {
	// Type is "function".
	Type     string             `json:"type"`
	Function FunctionDefinition `json:"function"`
}

// FunctionDefinition describes a function tool.
type FunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      bool            `json:"strict,omitempty"`
}

// ToolCall is a call the model made to a tool.
type ToolCall struct {
	ID string `json:"id"`
	// Type is "function".
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is a function tool call's function and its JSON arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ChatCompletion is the response to a non-streamed chat completion request.
type ChatCompletion struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   *Usage                 `json:"usage,omitempty"`
}

// ChatCompletionChoice is one of a chat completion's choices.
type ChatCompletionChoice struct {
	Index        int             `json:"index"`
	Message      ResponseMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

// ResponseMessage is the message a choice generated.
type ResponseMessage struct {
	Role string `json:"role"`
	// Content is null when the model only called tools.
	Content   *string    `json:"content"`
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`
}

// Usage is a completion's token usage.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChunk is one server-sent event of a streamed chat completion.
type ChatCompletionChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
	// Usage is only set on the final chunk, when the request asked for it.
	Usage *Usage `json:"usage,omitempty"`
}

// ChunkChoice is a choice's increment in a streamed chunk.
type ChunkChoice struct {
	Index int        `json:"index"`
	Delta ChunkDelta `json:"delta"`
	// FinishReason is null until the choice's last chunk.
	FinishReason *string `json:"finish_reason"`
}

// ChunkDelta is what a streamed chunk adds to a choice's message.
type ChunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ChunkToolCall `json:"tool_calls,omitempty"`
}

// ChunkToolCall is a tool call in a streamed chunk.
type ChunkToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// ModelList is the response to GET /models.
type ModelList struct {
	Object string        `json:"object"`
	Data   []ModelObject `json:"data"`
}

// ModelObject describes a model clients can request.
type ModelObject struct {
	// ID is the connection's name.
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	// OwnedBy is the connection's provider.
	OwnedBy string `json:"owned_by"`
}

// ModelListFor lists conns in the OpenAI models format.
func ModelListFor(conns []*Connection) ModelList {
	data := make([]ModelObject, len(conns))
	for i, conn := range conns {
		data[i] = ModelObject{ID: conn.Name, Object: "model", OwnedBy: string(conn.Provider)}
	}
	return ModelList{Object: "list", Data: data}
}

// LangchainMessages translates the request's messages to langchaingo's.
func (r *ChatCompletionRequest) LangchainMessages() ([]llms.MessageContent, error) {
	if len(r.Messages) == 0 {
		return nil, fmt.Errorf("%w: messages is required", ErrInvalidChatRequest)
	}
	out := make([]llms.MessageContent, 0, len(r.Messages))
	for i, m := range r.Messages {
		msg, err := m.toLangchain()
		if err != nil {
			return nil, fmt.Errorf("%w: messages[%d]: %w", ErrInvalidChatRequest, i, err)
		}
		out = append(out, msg)
	}
	return out, nil
}

func (m ChatMessage) toLangchain() (llms.MessageContent, error) {
	switch m.Role {
	case "system", "developer":
		return llms.MessageContent{
			Role:  llms.ChatMessageTypeSystem,
			Parts: []llms.ContentPart{llms.TextPart(m.Content.text())},
		}, nil
	case "user":
		parts, err := m.Content.toLangchain()
		if err != nil {
			return llms.MessageContent{}, err
		}
		return llms.MessageContent{Role: llms.ChatMessageTypeHuman, Parts: parts}, nil
	case "assistant":
		var parts []llms.ContentPart
		if text := m.Content.text(); text != "" {
			parts = append(parts, llms.TextPart(text))
		}
		for _, call := range m.ToolCalls {
			parts = append(parts, llms.ToolCall{
				ID:   call.ID,
				Type: "function",
				FunctionCall: &llms.FunctionCall{
					Name:      call.Function.Name,
					Arguments: call.Function.Arguments,
				},
			})
		}
		return llms.MessageContent{Role: llms.ChatMessageTypeAI, Parts: parts}, nil
	case "tool":
		if m.ToolCallID == "" {
			return llms.MessageContent{}, errors.New("tool message needs tool_call_id")
		}
		return llms.MessageContent{
			Role: llms.ChatMessageTypeTool,
			Parts: []llms.ContentPart{llms.ToolCallResponse{
				ToolCallID: m.ToolCallID,
				Name:       m.Name,
				Content:    m.Content.text(),
			}},
		}, nil
	default:
		return llms.MessageContent{}, fmt.Errorf("unsupported role %q", m.Role)
	}
}

func (c MessageContent) toLangchain() ([]llms.ContentPart, error) {
	parts := make([]llms.ContentPart, 0, len(c))
	for _, part := range c {
		switch part.Type {
		case "text":
			parts = append(parts, llms.TextPart(part.Text))
		case "image_url":
			if part.ImageURL == nil || part.ImageURL.URL == "" {
				return nil, errors.New("image_url part needs a url")
			}
			parts = append(
				parts,
				llms.ImageURLWithDetailPart(part.ImageURL.URL, part.ImageURL.Detail),
			)
		default:
			return nil, fmt.Errorf("unsupported content part type %q", part.Type)
		}
	}
	return parts, nil
}

// CallOptions translates the request's parameters to langchaingo call
// options. Streaming is left to the caller.
func (r *ChatCompletionRequest) CallOptions() ([]llms.CallOption, error) {
	var opts []llms.CallOption
	if r.Temperature != nil {
		opts = append(opts, llms.WithTemperature(*r.Temperature))
	}
	if r.TopP != nil {
		opts = append(opts, llms.WithTopP(*r.TopP))
	}
	// max_completion_tokens replaced max_tokens; prefer it when both are set.
	if r.MaxCompletionTokens != nil {
		opts = append(opts, llms.WithMaxTokens(*r.MaxCompletionTokens))
	} else if r.MaxTokens != nil {
		opts = append(opts, llms.WithMaxTokens(*r.MaxTokens))
	}
	if len(r.Stop) > 0 {
		opts = append(opts, llms.WithStopWords(r.Stop))
	}
	if r.N != nil {
		opts = append(opts, llms.WithN(*r.N))
	}
	if r.Seed != nil {
		opts = append(opts, llms.WithSeed(*r.Seed))
	}
	if r.FrequencyPenalty != nil {
		opts = append(opts, llms.WithFrequencyPenalty(*r.FrequencyPenalty))
	}
	if r.PresencePenalty != nil {
		opts = append(opts, llms.WithPresencePenalty(*r.PresencePenalty))
	}
	if len(r.Tools) > 0 {
		tools := make([]llms.Tool, len(r.Tools))
		for i, tool := range r.Tools {
			if tool.Type != "function" {
				return nil, fmt.Errorf(
					"%w: tools[%d]: unsupported tool type %q", ErrInvalidChatRequest, i, tool.Type,
				)
			}
			var params any
			if len(tool.Function.Parameters) > 0 {
				if err := json.Unmarshal(tool.Function.Parameters, &params); err != nil {
					return nil, fmt.Errorf("%w: tools[%d].function.parameters: %w",
						ErrInvalidChatRequest, i, err)
				}
			}
			tools[i] = llms.Tool{
				Type: "function",
				Function: &llms.FunctionDefinition{
					Name:        tool.Function.Name,
					Description: tool.Function.Description,
					Parameters:  params,
					Strict:      tool.Function.Strict,
				},
			}
		}
		opts = append(opts, llms.WithTools(tools))
	}
	if len(r.ToolChoice) > 0 {
		choice, err := toolChoice(r.ToolChoice)
		if err != nil {
			return nil, err
		}
		opts = append(opts, llms.WithToolChoice(choice))
	}
	if r.ResponseFormat != nil {
		switch r.ResponseFormat.Type {
		case "", "text":
		// langchaingo has no structured-output option common to every
		// provider, so a json_schema request gets JSON mode: valid JSON, but
		// not checked against the schema.
		case "json_object", "json_schema":
			opts = append(opts, llms.WithJSONMode())
		default:
			return nil, fmt.Errorf(
				"%w: unsupported response_format %q", ErrInvalidChatRequest, r.ResponseFormat.Type,
			)
		}
	}
	return opts, nil
}

// toolChoice translates tool_choice: a mode string passes through, and a
// named function becomes an llms.ToolChoice.
func toolChoice(raw json.RawMessage) (any, error) {
	var mode string
	if err := json.Unmarshal(raw, &mode); err == nil {
		return mode, nil
	}
	var named struct {
		Type     string `json:"type"`
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if err := json.Unmarshal(raw, &named); err != nil || named.Function.Name == "" {
		return nil, fmt.Errorf(
			"%w: tool_choice must be a mode or a named function",
			ErrInvalidChatRequest,
		)
	}
	return llms.ToolChoice{
		Type:     "function",
		Function: &llms.FunctionReference{Name: named.Function.Name},
	}, nil
}

// ChatCompletionFor translates resp to the OpenAI format. id, created and
// model identify the completion.
func ChatCompletionFor(
	resp *llms.ContentResponse,
	id string,
	created int64,
	model string,
) ChatCompletion {
	completion := ChatCompletion{
		ID:      id,
		Object:  "chat.completion",
		Created: created,
		Model:   model,
		Choices: make([]ChatCompletionChoice, len(resp.Choices)),
	}
	for i, choice := range resp.Choices {
		var content *string
		if choice.Content != "" || len(choice.ToolCalls) == 0 {
			content = &choice.Content
		}
		completion.Choices[i] = ChatCompletionChoice{
			Index: i,
			Message: ResponseMessage{
				Role:      "assistant",
				Content:   content,
				ToolCalls: toolCalls(choice),
			},
			FinishReason: FinishReason(choice),
		}
	}
	completion.Usage = UsageOf(resp)
	return completion
}

func toolCalls(choice *llms.ContentChoice) []ToolCall {
	if len(choice.ToolCalls) == 0 {
		return nil
	}
	calls := make([]ToolCall, 0, len(choice.ToolCalls))
	for _, call := range choice.ToolCalls {
		if call.FunctionCall == nil {
			continue
		}
		calls = append(calls, ToolCall{
			ID:   call.ID,
			Type: "function",
			Function: FunctionCall{
				Name:      call.FunctionCall.Name,
				Arguments: call.FunctionCall.Arguments,
			},
		})
	}
	return calls
}

// ToolCallChunks are choice's tool calls, as a streamed chunk carries them.
func ToolCallChunks(choice *llms.ContentChoice) []ChunkToolCall {
	calls := toolCalls(choice)
	if len(calls) == 0 {
		return nil
	}
	chunks := make([]ChunkToolCall, len(calls))
	for i, call := range calls {
		chunks[i] = ChunkToolCall{Index: i, ID: call.ID, Type: call.Type, Function: call.Function}
	}
	return chunks
}

// FinishReason normalizes choice's provider-specific stop reason to OpenAI's:
// stop, length, tool_calls or content_filter.
func FinishReason(choice *llms.ContentChoice) string {
	if len(choice.ToolCalls) > 0 {
		return "tool_calls"
	}
	switch strings.ToLower(choice.StopReason) {
	case "length", "max_tokens":
		return "length"
	case "tool_calls", "tool_use", "function_call":
		return "tool_calls"
	case "content_filter", "safety", "recitation", "blocklist", "prohibited_content":
		return "content_filter"
	default:
		return "stop"
	}
}

// UsageOf reads token usage from resp's first choice, where every langchaingo
// provider reports it, under OpenAI's names or Anthropic's. It returns nil if
// the provider reported none.
func UsageOf(resp *llms.ContentResponse) *Usage {
	if len(resp.Choices) == 0 {
		return nil
	}
	info := resp.Choices[0].GenerationInfo
	prompt, okPrompt := firstInt(info, "PromptTokens", "InputTokens")
	completion, okCompletion := firstInt(info, "CompletionTokens", "OutputTokens")
	if !okPrompt && !okCompletion {
		return nil
	}
	total, ok := firstInt(info, "TotalTokens")
	if !ok {
		total = prompt + completion
	}
	return &Usage{PromptTokens: prompt, CompletionTokens: completion, TotalTokens: total}
}

// firstInt returns the first of keys that info holds as an integer. Providers
// report counts as different integer types, so this switches on them.
func firstInt(info map[string]any, keys ...string) (int, bool) {
	for _, key := range keys {
		switch v := info[key].(type) {
		case int:
			return v, true
		case int32:
			return int(v), true
		case int64:
			return int(v), true
		case float64:
			return int(v), true
		}
	}
	return 0, false
}
