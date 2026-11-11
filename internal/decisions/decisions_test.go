package decisions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

// fakeServer serves /decisions with the given response body and records the
// request it received.
func fakeServer(t *testing.T, response string) (baseURL string, got *recorded) {
	t.Helper()
	got = &recorded{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.auth = r.Header.Get("Authorization")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got.body))
		w.Header().Set("Content-Type", "application/json")
		_, err := w.Write([]byte(response))
		require.NoError(t, err)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/providers/microsoft/v1/systemone", got
}

type recorded struct {
	path string
	auth string
	body map[string]any
}

func TestPredicatesSendsDeploymentConfigAndReturnsProbabilities(t *testing.T) {
	baseURL, got := fakeServer(t, `{
		"model": "microsoft-decision-1",
		"usage": {"input_tokens": 10, "output_tokens": 1},
		"answers": {
			"search": {"type": "noul", "noul": 0.9},
			"write": {"type": "noul", "noul": 0.1}
		}
	}`)
	c := New(baseURL, "secret", "my-deployment", http.DefaultClient)

	probs, err := c.Predicates(t.Context(), "find my notes", []Predicate{
		{Name: "search", Instructions: "Does the user want to search?"},
		{Name: "write", Instructions: "Does the user want to write?"},
	})

	require.NoError(t, err)
	require.Equal(t, map[string]float64{"search": 0.9, "write": 0.1}, probs)
	require.Equal(t, "/providers/microsoft/v1/systemone", got.path)
	require.Equal(t, "Bearer secret", got.auth)
	require.Equal(t, "my-deployment", got.body["model"])
	require.Equal(t, "find my notes", got.body["state"])
	require.Equal(t, map[string]any{
		"search": map[string]any{"type": "noul", "instructions": "Does the user want to search?"},
		"write":  map[string]any{"type": "noul", "instructions": "Does the user want to write?"},
	}, got.body["questions"])
}

func TestChooseReturnsMostLikelyOption(t *testing.T) {
	baseURL, got := fakeServer(t, `{
		"model": "m",
		"usage": {},
		"answers": {"q": {
			"type": "choice", "choice": "get_record", "confidence": 0.8,
			"probabilities": {"get_record": 0.8, "search": 0.2}
		}}
	}`)
	c := New(baseURL, "k", "m", http.DefaultClient)

	choice, err := c.Choose(t.Context(), "open this record", "Which tool?", []Option{
		{Value: "get_record", Description: "Fetch a record"},
		{Value: "search"},
	})

	require.NoError(t, err)
	require.Equal(t, Choice{
		Value:         "get_record",
		Confidence:    0.8,
		Probabilities: map[string]float64{"get_record": 0.8, "search": 0.2},
	}, choice)
	require.Equal(t, map[string]any{"q": map[string]any{
		"type":         "choice",
		"instructions": "Which tool?",
		"criteria":     map[string]any{"get_record": "Fetch a record", "search": nil},
	}}, got.body["questions"])
}

func TestMissingAnswerAndServerErrors(t *testing.T) {
	baseURL, _ := fakeServer(t, `{"model": "m", "usage": {}, "answers": {}}`)
	c := New(baseURL, "k", "m", http.DefaultClient)

	_, err := c.Predicates(t.Context(), "x", []Predicate{{Name: "q", Instructions: "?"}})
	require.ErrorIs(t, err, ErrRefused)

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, `{"error":{"message":"bad"}}`, http.StatusBadRequest)
	}))
	t.Cleanup(failing.Close)
	_, err = New(failing.URL, "k", "m", http.DefaultClient).
		Choose(t.Context(), "x", "?", []Option{{Value: "a"}, {Value: "b"}})
	require.Error(t, err)
}
