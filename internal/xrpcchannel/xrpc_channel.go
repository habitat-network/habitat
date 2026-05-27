package xrpcchannel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/pdsclient"
)

type XrpcChannel interface {
	SendXRPC(
		ctx context.Context,
		sender syntax.DID,
		receiver syntax.DID,
		req *http.Request,
	) (*http.Response, error)
}

type serviceProxyXrpcChannel struct {
	serviceName string
	directory   identity.Directory
	client      pdsclient.PdsOAuthClient
}

func NewServiceProxyXrpcChannel(
	serviceName string,
	client pdsclient.PdsOAuthClient,
	directory identity.Directory,
) XrpcChannel {
	return &serviceProxyXrpcChannel{
		serviceName: serviceName,
		client:      client,
		directory:   directory,
	}
}

func (m *serviceProxyXrpcChannel) SendXRPC(
	ctx context.Context,
	sender syntax.DID,
	receiver syntax.DID,
	req *http.Request,
) (*http.Response, error) {
	atid, err := m.directory.LookupDID(context.Background(), receiver)
	if err != nil {
		return nil, fmt.Errorf("[xrpc channel]: failed to lookup identity: %w", err)
	}
	pearServiceEndpoint := atid.GetServiceEndpoint(m.serviceName)
	u, err := url.Parse(pearServiceEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}
	pearServiceDid := fmt.Sprintf("did:web:%s#habitat", u.Hostname())
	req.Header.Set("atproto-proxy", pearServiceDid)

	return m.client.Do(ctx, sender, req)
}
