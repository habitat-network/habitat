package authn

import (
	"context"

	"github.com/bluesky-social/indigo/atproto/syntax"
)

func FromContext(ctx context.Context) (syntax.DID, bool) {
	did, ok := ctx.Value(callerKey).(syntax.DID)
	return did, ok
}
