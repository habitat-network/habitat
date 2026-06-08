package xrpcchannel

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bluesky-social/indigo/atproto/atcrypto"
	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdsclient"
	"github.com/stretchr/testify/require"
)

// testOAuthClient wraps *oauth.ClientApp to implement pdsclient.PdsOAuthClient.
type testOAuthClient struct {
	app *oauth.ClientApp
}

func (t *testOAuthClient) ClientMetadata() *oauth.ClientMetadata {
	meta := t.app.Config.ClientMetadata()
	return &meta
}

func (t *testOAuthClient) Authorize(ctx context.Context, identifier string) (string, error) {
	return t.app.StartAuthFlow(ctx, identifier)
}

func (t *testOAuthClient) ExchangeCode(ctx context.Context, code, issuer, state string) error {
	panic("unimplemented")
}

func (t *testOAuthClient) Do(ctx context.Context, did syntax.DID, req *http.Request) (*http.Response, error) {
	sess, err := t.app.ResumeSession(ctx, did, "default")
	if err != nil {
		return nil, err
	}
	nsidStr := strings.TrimPrefix(req.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		return nil, err
	}
	return sess.DoWithAuth(http.DefaultClient, req, nsid)
}

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

	client := &testOAuthClient{app: app}
	dir := app.Dir

	channel := NewServiceProxyXrpcChannel("habitat", client, dir)
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
