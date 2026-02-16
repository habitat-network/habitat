package xrpcchannel

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

type dummyChannel struct{}

func NewDummy() XrpcChannel {
	return &dummyChannel{}
}

func (c *dummyChannel) SendXRPC(
	ctx context.Context,
	sender syntax.DID,
	receiver syntax.DID,
	req *http.Request,
) (*http.Response, error) {
	panic("dummy for typing")
}
