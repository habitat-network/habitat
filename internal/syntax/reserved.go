package syntax

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

var (
	ReservedCollections = xmaps.Set[syntax.NSID]{UserRelationCollection: struct{}{}, SpaceRelationCollection: struct{}{}}
)
