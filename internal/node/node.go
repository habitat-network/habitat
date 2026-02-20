package node

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/habitat-network/habitat/internal/xrpcchannel"
)

// An abstraction serving federated PDS ownership. Can be used by any component to answer the question: do I own the records/permissions/whatever for this DID?
type Node interface {
	xrpcchannel.XrpcChannel // TODO, should these merge into one type instead of embedding?
	ServesDID(ctx context.Context, did syntax.DID) (bool, error)
}
