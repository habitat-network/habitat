package xrpcchannel

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/bluesky-social/indigo/atproto/auth/oauth"
	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
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
	clientApp   *oauth.ClientApp
}

func NewServiceProxyXrpcChannel(
	serviceName string,
	clientApp *oauth.ClientApp,
	directory identity.Directory,
) XrpcChannel {
	return &serviceProxyXrpcChannel{
		serviceName: serviceName,
		clientApp:   clientApp,
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

	sess, err := m.clientApp.ResumeSession(ctx, sender, "default")
	if err != nil {
		return nil, fmt.Errorf("failed to resume session: %w", err)
	}
	nsidStr := strings.TrimPrefix(req.URL.Path, "/xrpc/")
	nsid, err := syntax.ParseNSID(nsidStr)
	if err != nil {
		return nil, fmt.Errorf("parse nsid: %w", err)
	}
	return sess.DoWithAuth(http.DefaultClient, req, nsid)
}
