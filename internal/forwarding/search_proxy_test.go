package forwarding

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewSearchProxy_ForwardsRequestAndAuthHeader(t *testing.T) {
	var gotPath, gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"results":[]}`))
	}))
	defer upstream.Close()

	proxy, err := NewSearchProxy(upstream.URL)
	require.NoError(t, err)

	req := httptest.NewRequest(
		http.MethodGet, "/xrpc/network.habitat.search.query?q=budget", nil,
	)
	req.Header.Set("Authorization", "Bearer caller-token")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "/xrpc/network.habitat.search.query", gotPath)
	require.Equal(t, "Bearer caller-token", gotAuth)
}

func TestNewSearchProxy_InvalidHostIsError(t *testing.T) {
	_, err := NewSearchProxy("://not-a-valid-url")
	require.Error(t, err)
}
