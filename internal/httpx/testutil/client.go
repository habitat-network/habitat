package testutil

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/habitat-network/habitat/internal/utils"
	"github.com/stretchr/testify/require"
)

type TestXRPCClient struct {
	t *testing.T
}

func NewTestXRPCClient(t *testing.T, opts ...utils.Opt[TestXRPCClient]) *TestXRPCClient {
	return new(utils.ResolveOptions(TestXRPCClient{t: t}, opts))
}

func (c *TestXRPCClient) Procedure(handler http.Handler, input any, output any) int {
	c.t.Helper()
	inputBytes, err := json.Marshal(input)
	require.NoError(c.t, err)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(inputBytes)))
	if w.Body.Len() > 0 {
		require.NoError(c.t, json.Unmarshal(w.Body.Bytes(), output))
	}
	return w.Code
}

func (c *TestXRPCClient) Query(
	handler http.Handler,
	params url.Values,
	output any,
) int {
	c.t.Helper()
	w := httptest.NewRecorder()
	handler.ServeHTTP(
		w,
		httptest.NewRequest(http.MethodGet, "/?"+params.Encode(), http.NoBody),
	)
	if w.Body.Len() > 0 {
		require.NoError(c.t, json.Unmarshal(w.Body.Bytes(), output))
	}
	return w.Code
}
