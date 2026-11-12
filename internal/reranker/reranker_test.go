package reranker

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCohereRerank(t *testing.T) {
	var gotAuth string
	var gotBody struct {
		Model     string   `json:"model"`
		Query     string   `json:"query"`
		Documents []string `json:"documents"`
		TopN      *int     `json:"top_n"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		require.Equal(t, "/v2/rerank", r.URL.Path)
		require.NoError(t, json.NewDecoder(r.Body).Decode(&gotBody))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[` +
			`{"index":2,"relevance_score":0.9},{"index":0,"relevance_score":0.4}]}`))
	}))
	t.Cleanup(srv.Close)

	r := NewCohere(srv.URL, "key", "rerank-v3.5", srv.Client())
	got, err := r.Rerank(t.Context(), "q", []string{"a", "b", "c"}, 2)
	require.NoError(t, err)
	require.Equal(t, []int{2, 0}, got)
	require.Equal(t, "Bearer key", gotAuth)
	require.Equal(t, "rerank-v3.5", gotBody.Model)
	require.Equal(t, "q", gotBody.Query)
	require.Equal(t, []string{"a", "b", "c"}, gotBody.Documents)
	require.NotNil(t, gotBody.TopN)
	require.Equal(t, 2, *gotBody.TopN)
}

func TestCohereRerankErrors(t *testing.T) {
	status := http.StatusOK
	body := `{"results":[{"index":7,"relevance_score":0.9}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	r := NewCohere(srv.URL, "key", "m", srv.Client())

	_, err := r.Rerank(t.Context(), "q", []string{"a"}, 0)
	require.Error(t, err, "out of range index")

	status, body = http.StatusUnauthorized, `{"message":"invalid api token"}`
	_, err = r.Rerank(t.Context(), "q", []string{"a"}, 0)
	require.Error(t, err)
}

func TestCohereRerankEmpty(t *testing.T) {
	r := NewCohere("", "key", "m", nil)
	got, err := r.Rerank(t.Context(), "q", nil, 0)
	require.NoError(t, err)
	require.Empty(t, got)
}
