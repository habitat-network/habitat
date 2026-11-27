package syntax

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
	"github.com/bradenaw/juniper/xmaps"
)

// AppAccessCollection is the collection network.habitat.space.appAccess
// grant records are written into. See ReservedCollections.
const AppAccessCollection syntax.NSID = "network.habitat.space.appAccess"

const (
	AtgrpMetaSpaceType    syntax.NSID = "org.atgrp.meta"
	AtgrpMembersSpaceType syntax.NSID = "org.atgrp.members"
	AtgrpSpaceCollection  syntax.NSID = "org.atgrp.space"
	SpacePolicyCollection syntax.NSID = "com.atproto.space.policy"
)

// IsAtgrpConfigurationSpaceType reports whether only group provisioning
// may create a space of this type.
func IsAtgrpConfigurationSpaceType(spaceType syntax.NSID) bool {
	return spaceType == AtgrpMetaSpaceType || spaceType == AtgrpMembersSpaceType
}

var (
	ReservedCollections = xmaps.Set[syntax.NSID]{
		UserRelationCollection:  struct{}{},
		SpaceRelationCollection: struct{}{},
		AppAccessCollection:     struct{}{},
		AtgrpSpaceCollection:    struct{}{},
		SpacePolicyCollection:   struct{}{},
	}
)
