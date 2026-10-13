package searchtest

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"

	"github.com/habitat-network/habitat/internal/search"
)

// EmbedderDimensions is the size of the fake embedder's embeddings.
const EmbedderDimensions = 8

// synonyms maps words to the dimension they share, so that the fake embedder
// places them together the way a real model places words of similar meaning.
var synonyms = map[string]int{
	"car": 0, "automobile": 0, "vehicle": 0,
	"fruit": 1, "apple": 1, "banana": 1,
}

var (
	embedderOnce sync.Once
	embedderPort int
	embedderErr  error
)

// Embedder returns a [search.Embedder] for a fake OpenAI-compatible
// embeddings server that the shared Meilisearch server can reach. Words in
// [synonyms] embed close together, so a semantic search matches them where a
// keyword search wouldn't.
func Embedder(t *testing.T, semanticRatio, minScore float64) search.Embedder {
	t.Helper()
	// Starting the server first makes the container, if any, able to reach
	// the embedder.
	serverURL(t)
	port := startEmbedder(t)
	host := testcontainers.HostInternal
	if os.Getenv(URLEnv) != "" {
		host = "127.0.0.1"
	}
	return search.Embedder{
		URL:           fmt.Sprintf("http://%s:%d/v1/embeddings", host, port),
		APIKey:        "test-key",
		Model:         "fake-embedding",
		Dimensions:    EmbedderDimensions,
		SemanticRatio: semanticRatio,
		MinScore:      minScore,
	}
}

// startEmbedder starts the fake embeddings server once per process and
// returns its port. Like the Meilisearch container, it lives until the test
// binary exits.
func startEmbedder(t *testing.T) int {
	t.Helper()
	embedderOnce.Do(func() {
		var l net.Listener
		l, embedderErr = net.Listen("tcp", "0.0.0.0:0")
		if embedderErr != nil {
			return
		}
		embedderPort = l.Addr().(*net.TCPAddr).Port
		go func() { _ = http.Serve(l, http.HandlerFunc(serveEmbeddings)) }()
	})
	require.NoError(t, embedderErr)
	return embedderPort
}

type embeddingsRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embeddingsResponse struct {
	Data []embeddingData `json:"data"`
}

type embeddingData struct {
	Embedding []float32 `json:"embedding"`
	Index     int       `json:"index"`
}

// serveEmbeddings answers an OpenAI embeddings request.
func serveEmbeddings(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer test-key" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	var req embeddingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var resp embeddingsResponse
	for i, text := range req.Input {
		// Like OpenAI and Azure OpenAI, refuse empty inputs.
		if strings.TrimSpace(text) == "" {
			http.Error(w, fmt.Sprintf("Invalid 'input[%d]': input cannot be an empty string.", i), http.StatusBadRequest)
			return
		}
		resp.Data = append(resp.Data, embeddingData{Embedding: embed(text), Index: i})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// embed counts text's words per dimension: a synonym's shared dimension, or
// a hashed one past them.
func embed(text string) []float32 {
	v := make([]float32, EmbedderDimensions)
	shared := 2
	for _, word := range strings.Fields(strings.ToLower(text)) {
		if dim, ok := synonyms[word]; ok {
			v[dim]++
			continue
		}
		h := fnv.New32a()
		_, _ = h.Write([]byte(word))
		v[shared+int(h.Sum32()%uint32(EmbedderDimensions-shared))]++
	}
	// An all-zero vector has no direction for cosine similarity.
	if strings.TrimSpace(text) == "" {
		v[EmbedderDimensions-1] = 1
	}
	return v
}
