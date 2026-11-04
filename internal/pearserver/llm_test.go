package pearserver_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/fake"

	"github.com/habitat-network/habitat/api/habitat"
	httpx_testutil "github.com/habitat-network/habitat/internal/httpx/testutil"
	"github.com/habitat-network/habitat/internal/llmgateway"
	pearserver_testutil "github.com/habitat-network/habitat/internal/pearserver/testutil"
)

type fakeLLM struct{ *fake.LLM }

func (fakeLLM) Close() error { return nil }

func fakeModels(context.Context, llmgateway.ModelConfig) (llmgateway.Model, error) {
	return fakeLLM{fake.NewFakeLLM([]string{"hello there"})}, nil
}

func TestServerLlmConnectionsRequireConfigureAction(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, alice)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	var none struct{}
	require.Equal(t, http.StatusUnauthorized, client.Procedure(
		ts.Server.AddLlmConnection,
		habitat.NetworkHabitatLlmAddConnectionInput{
			Org: orgDID, Name: "gpt", Provider: "openai", Model: "gpt-4o", ApiKey: "k",
		},
		&none,
	))
}

func TestServerLlmConnectionsAdminFlow(t *testing.T) {
	client := httpx_testutil.NewTestXRPCClient(t)
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)

	var added habitat.NetworkHabitatLlmAddConnectionOutput
	require.Equal(t, http.StatusOK, client.Procedure(
		ts.Server.AddLlmConnection,
		habitat.NetworkHabitatLlmAddConnectionInput{
			Org: orgDID, Name: "gpt", Provider: "openai", Model: "gpt-4o", ApiKey: "sk-secret",
		},
		&added,
	))
	require.True(t, added.Connection.HasApiKey)

	// A bad provider is the caller's mistake.
	var none struct{}
	require.Equal(t, http.StatusBadRequest, client.Procedure(
		ts.Server.AddLlmConnection,
		habitat.NetworkHabitatLlmAddConnectionInput{
			Org: orgDID, Name: "x", Provider: "nope", Model: "m", ApiKey: "k",
		},
		&none,
	))

	var updated habitat.NetworkHabitatLlmUpdateConnectionOutput
	require.Equal(t, http.StatusOK, client.Procedure(
		ts.Server.UpdateLlmConnection,
		habitat.NetworkHabitatLlmUpdateConnectionInput{Org: orgDID, Id: "gpt", Model: "gpt-4.1"},
		&updated,
	))
	require.Equal(t, "gpt-4.1", updated.Connection.Model)

	var listed habitat.NetworkHabitatLlmListConnectionsOutput
	require.Equal(t, http.StatusOK, client.Query(
		ts.Server.ListLlmConnections, url.Values{"org": {orgDID}}, &listed,
	))
	require.Len(t, listed.Connections, 1)
	// The key is never returned.
	raw, err := json.Marshal(listed)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "sk-secret")

	require.Equal(t, http.StatusOK, client.Procedure(
		ts.Server.RemoveLlmConnection,
		habitat.NetworkHabitatLlmRemoveConnectionInput{Org: orgDID, Id: "gpt"},
		&none,
	))
	require.Equal(t, http.StatusNotFound, client.Procedure(
		ts.Server.RemoveLlmConnection,
		habitat.NetworkHabitatLlmRemoveConnectionInput{Org: orgDID, Id: "gpt"},
		&none,
	))
}

func TestServerLlmOpenAICompatibleAPI(t *testing.T) {
	ts := newOpenSocialServer(t, admin)
	orgDID, err := ts.OpenSocialStore.NewOrg(t.Context(), "acme", admin)
	require.NoError(t, err)
	// Rebuild with fake models over the same stores.
	ts = pearserver_testutil.NewTestServer(t,
		pearserver_testutil.WithDB(ts.DB),
		pearserver_testutil.WithFGA(ts.FGA),
		pearserver_testutil.WithSpaceStore(ts.SpaceStore),
		pearserver_testutil.WithValidator(ts.Validator),
		pearserver_testutil.WithLLMModelFactory(fakeModels),
	)
	_, err = ts.LLMStore.Add(t.Context(), syntax.DID(orgDID), llmgateway.NewConnection{
		Name: "gpt", Provider: llmgateway.ProviderOpenAI, Model: "gpt-4o", APIKey: "k",
	})
	require.NoError(t, err)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/llm/"+orgDID+"/v1"+path, strings.NewReader(body))
		w := httptest.NewRecorder()
		ts.Server.ServeHTTP(w, req)
		return w
	}

	t.Run("lists models", func(t *testing.T) {
		w := do(http.MethodGet, "/models", "")
		require.Equal(t, http.StatusOK, w.Code)
		var list llmgateway.ModelList
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
		require.Equal(t, "list", list.Object)
		require.Equal(t, "gpt", list.Data[0].ID)
	})

	t.Run("completes a chat", func(t *testing.T) {
		w := do(http.MethodPost, "/chat/completions",
			`{"model":"gpt","messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusOK, w.Code)
		var got llmgateway.ChatCompletion
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
		require.Equal(t, "hello there", *got.Choices[0].Message.Content)
		require.Equal(t, "stop", got.Choices[0].FinishReason)
		require.Equal(t, "gpt", got.Model)
	})

	t.Run("streams a chat", func(t *testing.T) {
		w := do(http.MethodPost, "/chat/completions",
			`{"model":"gpt","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusOK, w.Code)
		require.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
		body := w.Body.String()
		require.Contains(t, body, `"content":"hello there"`)
		require.Contains(t, body, `"finish_reason":"stop"`)
		require.True(t, strings.HasSuffix(body, "data: [DONE]\n\n"))
	})

	t.Run("unknown model is an OpenAI-format 404", func(t *testing.T) {
		w := do(http.MethodPost, "/chat/completions",
			`{"model":"nope","messages":[{"role":"user","content":"hi"}]}`)
		require.Equal(t, http.StatusNotFound, w.Code)
		require.Contains(t, w.Body.String(), `"code":"model_not_found"`)
	})

	t.Run("rejects an invalid request", func(t *testing.T) {
		w := do(http.MethodPost, "/chat/completions", `{"model":"gpt","messages":[]}`)
		require.Equal(t, http.StatusBadRequest, w.Code)
		require.Contains(t, w.Body.String(), "invalid_request_error")
	})
}
