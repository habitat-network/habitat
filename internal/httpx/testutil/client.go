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

func (c *TestXRPCClient) Do(handler http.HandlerFunc, req *http.Request) httptest.ResponseRecorder {
	c.t.Helper()
	w := httptest.NewRecorder()
	handler(w, req)
	return *w
}

func (c *TestXRPCClient) unmarshalResponse(
	w httptest.ResponseRecorder,
	output any,
) int {
	c.t.Helper()
	if w.Body.Len() > 0 {
		require.NoError(c.t, json.Unmarshal(w.Body.Bytes(), output))
	}
	return w.Code
}

func (c *TestXRPCClient) Procedure(handler http.HandlerFunc, input any, output any) int {
	c.t.Helper()
	inputBytes, err := json.Marshal(input)
	require.NoError(c.t, err)
	return c.unmarshalResponse(
		c.Do(handler, httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(inputBytes))),
		output,
	)
}

func (c *TestXRPCClient) Query(
	handler http.HandlerFunc,
	params url.Values,
	output any,
) int {
	c.t.Helper()
	return c.unmarshalResponse(
		c.Do(handler, httptest.NewRequest(http.MethodGet, "/?"+params.Encode(), http.NoBody)),
		output,
	)
}
