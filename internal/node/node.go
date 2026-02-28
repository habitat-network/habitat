package node

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/identity"
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
)

// An abstraction serving federated PDS ownership. Can be used by any component to answer the question: do I own the records/permissions/whatever for this DID?
type Node interface {
	xrpcchannel.XrpcChannel // TODO, should these merge into one type instead of embedding?
	ServesDID(ctx context.Context, did syntax.DID) (bool, error)
}

type node struct {
	serviceName     string
	serviceEndpoint string

	dir    identity.Directory
	xrpcCh xrpcchannel.XrpcChannel
}

var _ Node = &node{}

func New(serviceName, serviceEndpoint string, dir identity.Directory, xrpcCh xrpcchannel.XrpcChannel) Node {
	return &node{
		serviceName:     serviceName,
		serviceEndpoint: serviceEndpoint,
		dir:             dir,
		xrpcCh:          xrpcCh,
	}
}

// SendXRPC implements Node.
func (n *node) SendXRPC(ctx context.Context, sender syntax.DID, receiver syntax.DID, req *http.Request) (*http.Response, error) {
	return n.xrpcCh.SendXRPC(ctx, sender, receiver, req)
}

var (
	ErrNoHabitatServer = errors.New("no habitat server found for did :%s")
)

// ServesDID implements Node.
func (n *node) ServesDID(ctx context.Context, did syntax.DID) (bool, error) {
	// Use context.Background() to avoid cached context cancelled errors: https://github.com/bluesky-social/indigo/pull/1345
	id, err := n.dir.LookupDID(context.Background(), did)
	if errors.Is(err, identity.ErrDIDNotFound) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	found, ok := id.Services[n.serviceName]
	if !ok {
		return false, fmt.Errorf(ErrNoHabitatServer.Error(), did.String())
	}

	return found.URL == n.serviceEndpoint, nil
}
