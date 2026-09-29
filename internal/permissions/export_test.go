package permissions

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

// This file exposes permissions' unexported store internals to its external
// tests. It is compiled only into the test binary, so none of it is part of the
// package's API.

// ListPermissions calls the store's unexported listPermissions, which the tests
// assert against directly rather than through a public wrapper.
func ListPermissions(
	s Store,
	ctx context.Context,
	grantees []Grantee,
	owners []syntax.DID,
	collection syntax.NSID,
	rkey syntax.RecordKey,
) ([]Permission, error) {
	return s.(*store).listPermissions(ctx, grantees, owners, collection, rkey)
}
