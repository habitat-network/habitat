package syntax

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

// AppAccessCollection is the collection network.habitat.space.appAccess
// grant records are written into. See ReservedCollections.
const AppAccessCollection syntax.NSID = "network.habitat.space.appAccess"

var (
	ReservedCollections = xmaps.Set[syntax.NSID]{
		UserRelationCollection:  struct{}{},
		SpaceRelationCollection: struct{}{},
		AppAccessCollection:     struct{}{},
	}
)
