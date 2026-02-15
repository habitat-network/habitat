package xrpcchannel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/oauthclient"
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
	serviceName   string
	directory     identity.Directory
	clientFactory oauthclient.PDSClientFactory
}

func NewServiceProxyXrpcChannel(
	serviceName string,
	clientFactory oauthclient.PDSClientFactory,
	directory identity.Directory,
) XrpcChannel {
	return &serviceProxyXrpcChannel{
		serviceName:   serviceName,
		clientFactory: clientFactory,
		directory:     directory,
	}
}

// SendXRPC implements [XrpcChannel].
func (m *serviceProxyXrpcChannel) SendXRPC(
	ctx context.Context,
	sender syntax.DID,
	receiver syntax.DID,
	req *http.Request,
) (*http.Response, error) {
	atid, err := m.directory.LookupDID(ctx, receiver)
	if err != nil {
		return nil, fmt.Errorf("failed to lookup identity: %w", err)
	}
	pearServiceEndpoint := atid.GetServiceEndpoint(m.serviceName)
	url, err := url.Parse(pearServiceEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to parse url: %w", err)
	}
	pearServiceDid := fmt.Sprintf("did:web:%s#habitat", url.Hostname())
	client, err := m.clientFactory.NewClient(req.Context(), sender)
	if err != nil {
		return nil, fmt.Errorf("failed to create client: %w", err)
	}
	req.Header.Set("atproto-proxy", pearServiceDid)
	return client.Do(req)
}
