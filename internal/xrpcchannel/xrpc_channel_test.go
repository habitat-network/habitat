package xrpcchannel

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/habitat-network/habitat/internal/oauthclient"
	"github.com/stretchr/testify/require"
)

func TestServiceProxyXrpcChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.getRecord", r.URL.Path)
		require.Equal(t, "did:web:habitat.network#habitat", r.Header.Get("atproto-proxy"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	channel := NewServiceProxyXrpcChannel(
		"habitat",
		oauthclient.NewDummyClientFactory(server.URL),
		oauthclient.NewDummyDirectory(server.URL),
	)
	req, err := http.NewRequest("GET", "/xrpc/network.habitat.getRecord", nil)
	require.NoError(t, err)
	resp, err := channel.SendXRPC(
		t.Context(),
		"did:plc:sender",
		"did:plc:receiver",
		req,
	)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
}
