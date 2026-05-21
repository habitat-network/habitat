package xrpcchannel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

func TestServiceProxyXrpcChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/xrpc/network.habitat.repo.getRecord", r.URL.Path)
		require.Equal(t, "did:web:habitat.network#habitat", r.Header.Get("atproto-proxy"))
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte("hello"))
		require.NoError(t, err)
	}))
	defer server.Close()

	dpopKey, err := atcrypto.GeneratePrivateKeyP256()
	require.NoError(t, err)

	store := oauth.NewMemStore()
	sessData := oauth.ClientSessionData{
		AccountDID:              "did:plc:sender",
		SessionID:               "default",
		HostURL:                 server.URL,
		AuthServerURL:           server.URL,
		AuthServerTokenEndpoint: server.URL + "/token",
		AccessToken:             "test-access-token",
		RefreshToken:            "test-refresh-token",
		Scopes:                  []string{"atproto"},
		DPoPPrivateKeyMultibase: dpopKey.Multibase(),
	}
	err = store.SaveSession(context.Background(), sessData)
	require.NoError(t, err)

	config := oauth.NewPublicConfig(
		"https://test.example.com/client-metadata.json",
		"https://test.example.com/oauth-callback",
		[]string{"atproto"},
	)
	app := oauth.NewClientApp(&config, store)
	app.Dir = pdsclient.NewDummyDirectory(server.URL, pdsclient.WithHabitatService())

	channel := NewServiceProxyXrpcChannel("habitat", app, app.Dir)
	req, err := http.NewRequest("GET", server.URL+"/xrpc/network.habitat.repo.getRecord", nil)
	require.NoError(t, err)
	resp, err := channel.SendXRPC(
		t.Context(),
		"did:plc:sender",
		"did:plc:receiver",
		req,
	)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "hello", string(body))
	require.NoError(t, resp.Body.Close())
}
