package pearserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/tmc/langchaingo/llms"

	"github.com/habitat-network/habitat/internal/authn"
	"github.com/habitat-network/habitat/internal/httpx"
	"github.com/habitat-network/habitat/internal/llmgateway"
)

// The org's OpenAI-compatible API is served at llmBasePath/{org}, so a client
// takes https://<host>/llm/<org DID>/v1 as its base URL and any OAuth access
// token as its API key.
const (
	llmModelsPath          = "/llm/{org}/v1/models"
	llmChatCompletionsPath = "/llm/{org}/v1/chat/completions"
)

// maxChatRequestBytes bounds a chat completion request body; image content
// arrives inline as data URLs, so it's generous.
const maxChatRequestBytes = 32 << 20

// openAIError is the error body OpenAI clients expect.
type openAIError struct {
	Error openAIErrorDetail `json:"error"`
}

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

func writeOpenAIError(
	w http.ResponseWriter,
	status int,
	errType, code, msg string,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(openAIError{Error: openAIErrorDetail{
		Message: msg, Type: errType, Code: code,
	}})
}

// authenticateLLM validates an OpenAI-style request: the access token is sent
// as the API key (Authorization: Bearer), and the caller must be a member of
// the org in the path. It writes the OpenAI-format error and returns false
// otherwise.
func (p *PearServer) authenticateLLM(w http.ResponseWriter, r *http.Request) (syntax.DID, bool) {
	ctx := r.Context()
	org, err := syntax.ParseDID(mux.Vars(r)["org"])
	if err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "invalid_org",
			"the org in the URL must be a DID")
		return "", false
	}
	// The validator writes its own (XRPC-format) 401 on failure.
	credInfo, ok := p.validator.Request(
		authn.WithMethods(authn.ValidatorMethodOAuth, authn.ValidatorMethodServiceAuth),
	).Validate(w, r)
	if !ok {
		return "", false
	}
	roles, err := p.opensocialStore.GetUserRoles(ctx, org, credInfo.Subject)
	if err != nil {
		slog.ErrorContext(ctx, "get user roles", "err", err)
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "", "internal error")
		return "", false
	}
	annotateAuthz(ctx, org, "member", len(roles) > 0)
	if len(roles) == 0 {
		writeOpenAIError(w, http.StatusForbidden, "invalid_request_error", "not_a_member",
			"caller is not a member of this community")
		return "", false
	}
	return org, true
}

// ListLlmModels implements the OpenAI GET /v1/models, listing the org's
// connections.
func (p *PearServer) ListLlmModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	org, ok := p.authenticateLLM(w, r)
	if !ok {
		return
	}
	conns, err := p.llmStore.List(ctx, org)
	if err != nil {
		slog.ErrorContext(ctx, "list llm connections", "err", err)
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "", "internal error")
		return
	}
	httpx.WriteJSON(ctx, w, llmgateway.ModelListFor(conns))
}

// CreateLlmChatCompletion implements the OpenAI POST /v1/chat/completions,
// routing the request to the org's connection named by its model.
func (p *PearServer) CreateLlmChatCompletion(w http.ResponseWriter, r *http.Request) {
	org, ok := p.authenticateLLM(w, r)
	if !ok {
		return
	}

	var req llmgateway.ChatCompletionRequest
	r.Body = http.MaxBytesReader(w, r.Body, maxChatRequestBytes)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "", "invalid request body: "+err.Error())
		return
	}
	messages, err := req.LangchainMessages()
	if err == nil {
		var opts []llms.CallOption
		if opts, err = req.CallOptions(); err == nil {
			p.completeChat(w, r, org, &req, messages, opts)
			return
		}
	}
	writeOpenAIError(w, http.StatusBadRequest, "invalid_request_error", "", err.Error())
}

func (p *PearServer) completeChat(
	w http.ResponseWriter,
	r *http.Request,
	org syntax.DID,
	req *llmgateway.ChatCompletionRequest,
	messages []llms.MessageContent,
	opts []llms.CallOption,
) {
	ctx := r.Context()
	id, err := syntax.ParseRecordKey(req.Model)
	if err != nil {
		writeOpenAIError(w, http.StatusNotFound, "invalid_request_error", "model_not_found",
			fmt.Sprintf("the model %q does not exist", req.Model))
		return
	}
	model, err := p.llmStore.Model(ctx, org, id)
	if errors.Is(err, llmgateway.ErrConnectionNotFound) {
		writeOpenAIError(w, http.StatusNotFound, "invalid_request_error", "model_not_found",
			fmt.Sprintf("the model %q does not exist", req.Model))
		return
	} else if err != nil {
		slog.ErrorContext(ctx, "build llm model", "err", err)
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "", "internal error")
		return
	}
	defer func() { _ = model.Close() }()

	completionID := "chatcmpl-" + uuid.NewString()
	created := time.Now().Unix()

	// Providers stream tool calls as raw JSON fragments rather than text, so a
	// streamed request with tools is answered by one upstream call replayed as
	// events instead.
	if !req.Stream || len(req.Tools) > 0 {
		resp, err := model.GenerateContent(ctx, messages, opts...)
		if err != nil {
			writeUpstreamError(ctx, w, err)
			return
		}
		completion := llmgateway.ChatCompletionFor(resp, completionID, created, req.Model)
		if req.Stream {
			streamCompletion(ctx, w, completion, req.StreamOptions)
			return
		}
		httpx.WriteJSON(ctx, w, completion)
		return
	}

	sse, ok := newSSEWriter(w)
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "", "streaming unsupported")
		return
	}
	chunk := func(choices []llmgateway.ChunkChoice, usage *llmgateway.Usage) llmgateway.ChatCompletionChunk {
		return llmgateway.ChatCompletionChunk{
			ID: completionID, Object: "chat.completion.chunk", Created: created,
			Model: req.Model, Choices: choices, Usage: usage,
		}
	}
	started := false
	start := func() error {
		started = true
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)
		return sse.send(chunk([]llmgateway.ChunkChoice{
			{Delta: llmgateway.ChunkDelta{Role: "assistant"}},
		}, nil))
	}
	opts = append(opts, llms.WithStreamingFunc(func(_ context.Context, text []byte) error {
		if !started {
			if err := start(); err != nil {
				return err
			}
		}
		return sse.send(chunk([]llmgateway.ChunkChoice{
			{Delta: llmgateway.ChunkDelta{Content: string(text)}},
		}, nil))
	}))
	resp, err := model.GenerateContent(ctx, messages, opts...)
	if err != nil {
		if !started {
			writeUpstreamError(ctx, w, err)
			return
		}
		// The 200 is already sent; surface the failure in the stream, as
		// OpenAI does.
		slog.ErrorContext(ctx, "llm stream", "err", err)
		_ = sse.sendRaw(openAIError{Error: openAIErrorDetail{
			Message: "upstream error", Type: "server_error",
		}})
		return
	}
	if !started {
		if err := start(); err != nil {
			return
		}
	}
	reason := "stop"
	if len(resp.Choices) > 0 {
		reason = llmgateway.FinishReason(resp.Choices[0])
	}
	if err := sse.send(chunk([]llmgateway.ChunkChoice{{FinishReason: &reason}}, nil)); err != nil {
		return
	}
	if req.StreamOptions != nil && req.StreamOptions.IncludeUsage {
		if err := sse.send(chunk(nil, llmgateway.UsageOf(resp))); err != nil {
			return
		}
	}
	_ = sse.done()
}

// streamCompletion replays an already complete completion as server-sent
// events.
func streamCompletion(
	ctx context.Context,
	w http.ResponseWriter,
	completion llmgateway.ChatCompletion,
	options *llmgateway.StreamOptions,
) {
	sse, ok := newSSEWriter(w)
	if !ok {
		writeOpenAIError(w, http.StatusInternalServerError, "server_error", "", "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	chunk := func(choices []llmgateway.ChunkChoice, usage *llmgateway.Usage) llmgateway.ChatCompletionChunk {
		return llmgateway.ChatCompletionChunk{
			ID: completion.ID, Object: "chat.completion.chunk", Created: completion.Created,
			Model: completion.Model, Choices: choices, Usage: usage,
		}
	}
	for _, choice := range completion.Choices {
		delta := llmgateway.ChunkDelta{Role: "assistant"}
		if choice.Message.Content != nil {
			delta.Content = *choice.Message.Content
		}
		for i, call := range choice.Message.ToolCalls {
			delta.ToolCalls = append(delta.ToolCalls, llmgateway.ChunkToolCall{
				Index: i, ID: call.ID, Type: call.Type, Function: call.Function,
			})
		}
		reason := choice.FinishReason
		if err := sse.send(chunk([]llmgateway.ChunkChoice{
			{Index: choice.Index, Delta: delta},
			{Index: choice.Index, FinishReason: &reason},
		}, nil)); err != nil {
			slog.WarnContext(ctx, "write llm stream", "err", err)
			return
		}
	}
	if options != nil && options.IncludeUsage {
		if err := sse.send(chunk(nil, completion.Usage)); err != nil {
			return
		}
	}
	_ = sse.done()
}

// writeUpstreamError reports a provider failure. The provider's own message is
// passed on, since it's what tells the caller why (bad model, rate limit); API
// keys never appear in it, as providers don't echo them.
func writeUpstreamError(ctx context.Context, w http.ResponseWriter, err error) {
	slog.WarnContext(ctx, "llm upstream error", "err", err)
	msg := err.Error()
	status := http.StatusBadGateway
	if strings.Contains(msg, "context canceled") {
		status = http.StatusRequestTimeout
	}
	writeOpenAIError(w, status, "api_error", "upstream_error", msg)
}

// sseWriter writes server-sent events and flushes each one.
type sseWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
}

func newSSEWriter(w http.ResponseWriter) (*sseWriter, bool) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, false
	}
	return &sseWriter{w: w, flusher: flusher}, true
}

func (s *sseWriter) send(chunk llmgateway.ChatCompletionChunk) error {
	return s.sendRaw(chunk)
}

func (s *sseWriter) sendRaw(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "data: %s\n\n", data); err != nil {
		return fmt.Errorf("write sse event: %w", err)
	}
	s.flusher.Flush()
	return nil
}

func (s *sseWriter) done() error {
	if _, err := fmt.Fprint(s.w, "data: [DONE]\n\n"); err != nil {
		return fmt.Errorf("write sse done: %w", err)
	}
	s.flusher.Flush()
	return nil
}
