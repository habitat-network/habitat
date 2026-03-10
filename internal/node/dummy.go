package node

import (
	"context"
	"net/http"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// This dummy node always returns true for ServesDID and panics for SendXRPC. Meant for test usage only.
type dummy struct{}

// SendXRPC implements Node.
func (d *dummy) SendXRPC(ctx context.Context, sender syntax.DID, receiver syntax.DID, req *http.Request) (*http.Response, error) {
	panic("unimplemented")
}

// ServesDID implements Node.
func (d *dummy) ServesDID(ctx context.Context, did syntax.DID) (bool, error) {
	return true, nil
}

var _ Node = &dummy{}

func NewDummy() Node {
	return &dummy{}
}
