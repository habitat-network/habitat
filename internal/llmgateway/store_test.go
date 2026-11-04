package llmgateway_test

import (
	"context"
	"testing"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/stretchr/testify/require"
	"github.com/tmc/langchaingo/llms/fake"

	pear_testutil "github.com/habitat-network/habitat/cmd/pear/testutil"
	"github.com/habitat-network/habitat/internal/encrypt"
	"github.com/habitat-network/habitat/internal/keyval"
	keyval_testutil "github.com/habitat-network/habitat/internal/keyval/testutil"
	"github.com/habitat-network/habitat/internal/llmgateway"
	"github.com/habitat-network/habitat/internal/opensocial"
	spaces_testutil "github.com/habitat-network/habitat/internal/spaces/testutil"
)

type fakeModel struct {
	*fake.LLM
	config llmgateway.ModelConfig
}

func (fakeModel) Close() error { return nil }

func newStore(t *testing.T, orgs ...syntax.DID) (*llmgateway.Store, *[]llmgateway.ModelConfig) {
	t.Helper()
	spaces := spaces_testutil.NewTestStore(
		t, spaces_testutil.WithDB(pear_testutil.NewPearDB(t)),
	)
	for _, org := range orgs {
		_, err := spaces.CreateSpace(t.Context(), org, opensocial.MembersSpaceType, "self")
		require.NoError(t, err)
	}
	kv := keyval_testutil.New(t)
	creds, err := keyval.NewEncrypted(
		kv.Namespace(llmgateway.CredentialsNamespace),
		encrypt.TestKey,
	)
	require.NoError(t, err)
	var built []llmgateway.ModelConfig
	factory := func(_ context.Context, c llmgateway.ModelConfig) (llmgateway.Model, error) {
		built = append(built, c)
		return fakeModel{LLM: fake.NewFakeLLM([]string{"hi"}), config: c}, nil
	}
	return llmgateway.NewStore(
		spaces, creds, kv.Namespace(llmgateway.CredentialsNamespace), factory,
	), &built
}

func TestStoreConnectionLifecycle(t *testing.T) {
	orgA := syntax.DID("did:web:a.example.com")
	orgB := syntax.DID("did:web:b.example.com")
	s, built := newStore(t, orgA, orgB)

	conn, err := s.Add(t.Context(), orgA, llmgateway.NewConnection{
		Name: "gpt", Provider: llmgateway.ProviderOpenAI, Model: "gpt-4o", APIKey: "sk-secret",
	})
	require.NoError(t, err)
	require.True(t, conn.HasAPIKey)
	_, err = s.Add(t.Context(), orgA, llmgateway.NewConnection{
		Name: "local", Provider: llmgateway.ProviderOllama, Model: "llama3",
		BaseURL: "http://localhost:11434",
	})
	require.NoError(t, err)

	// Connections are scoped to their org.
	listed, err := s.List(t.Context(), orgA)
	require.NoError(t, err)
	require.Len(t, listed, 2)
	require.Equal(t, "gpt", listed[0].Name)
	require.True(t, listed[0].HasAPIKey)
	require.False(t, listed[1].HasAPIKey)
	listed, err = s.List(t.Context(), orgB)
	require.NoError(t, err)
	require.Empty(t, listed)
	_, err = s.Get(t.Context(), orgB, "gpt")
	require.ErrorIs(t, err, llmgateway.ErrConnectionNotFound)

	// The model is built with the stored key.
	_, err = s.Model(t.Context(), orgA, "gpt")
	require.NoError(t, err)
	require.Equal(t, llmgateway.ModelConfig{
		Provider: llmgateway.ProviderOpenAI, Model: "gpt-4o", APIKey: "sk-secret",
	}, (*built)[0])

	// Updating changes the model and rotates the key; absent fields stay.
	newModel, newKey := "gpt-4.1", "sk-rotated"
	updated, err := s.Update(t.Context(), orgA, "gpt", llmgateway.ConnectionUpdate{
		Model: &newModel, APIKey: &newKey,
	})
	require.NoError(t, err)
	require.Equal(t, "gpt-4.1", updated.Model)
	_, err = s.Model(t.Context(), orgA, "gpt")
	require.NoError(t, err)
	require.Equal(t, "sk-rotated", (*built)[1].APIKey)

	// Clearing the key of a provider that needs one is refused.
	empty := ""
	_, err = s.Update(t.Context(), orgA, "gpt", llmgateway.ConnectionUpdate{APIKey: &empty})
	require.ErrorIs(t, err, llmgateway.ErrMissingAPIKey)

	require.NoError(t, s.Remove(t.Context(), orgA, "gpt"))
	_, err = s.Get(t.Context(), orgA, "gpt")
	require.ErrorIs(t, err, llmgateway.ErrConnectionNotFound)
	require.ErrorIs(t, s.Remove(t.Context(), orgA, "gpt"), llmgateway.ErrConnectionNotFound)
	// Re-adding under the same name doesn't resurrect the removed key.
	_, err = s.Add(t.Context(), orgA, llmgateway.NewConnection{
		Name: "gpt", Provider: llmgateway.ProviderOpenAI, Model: "gpt-4o", APIKey: "sk-new",
	})
	require.NoError(t, err)
	_, err = s.Model(t.Context(), orgA, "gpt")
	require.NoError(t, err)
	require.Equal(t, "sk-new", (*built)[2].APIKey)
}

func TestStoreAddValidation(t *testing.T) {
	org := syntax.DID("did:web:a.example.com")
	s, _ := newStore(t, org)
	valid := llmgateway.NewConnection{
		Name: "gpt", Provider: llmgateway.ProviderOpenAI, Model: "gpt-4o", APIKey: "k",
	}
	_, err := s.Add(t.Context(), org, valid)
	require.NoError(t, err)

	for name, tc := range map[string]struct {
		mutate func(*llmgateway.NewConnection)
		want   error
	}{
		"duplicate name":   {func(c *llmgateway.NewConnection) {}, llmgateway.ErrNameTaken},
		"bad name":         {func(c *llmgateway.NewConnection) { c.Name = "a b" }, llmgateway.ErrInvalidName},
		"dot name":         {func(c *llmgateway.NewConnection) { c.Name = ".." }, llmgateway.ErrInvalidName},
		"unknown provider": {func(c *llmgateway.NewConnection) { c.Name = "x"; c.Provider = "nope" }, llmgateway.ErrInvalidProvider},
		"missing model":    {func(c *llmgateway.NewConnection) { c.Name = "x"; c.Model = " " }, llmgateway.ErrMissingModel},
		"bad base url":     {func(c *llmgateway.NewConnection) { c.Name = "x"; c.BaseURL = "ftp://x" }, llmgateway.ErrInvalidBaseURL},
		"missing key":      {func(c *llmgateway.NewConnection) { c.Name = "x"; c.APIKey = "" }, llmgateway.ErrMissingAPIKey},
		"ollama needs url": {func(c *llmgateway.NewConnection) { c.Name = "x"; c.Provider = llmgateway.ProviderOllama; c.APIKey = "" }, llmgateway.ErrInvalidBaseURL},
		"ollama takes no key": {func(c *llmgateway.NewConnection) {
			c.Name = "x"
			c.Provider = llmgateway.ProviderOllama
			c.BaseURL = "http://h"
		}, llmgateway.ErrAPIKeyNotSupported},
	} {
		t.Run(name, func(t *testing.T) {
			c := valid
			tc.mutate(&c)
			_, err := s.Add(t.Context(), org, c)
			require.ErrorIs(t, err, tc.want)
			require.True(t, llmgateway.IsConfigError(err))
		})
	}
}
