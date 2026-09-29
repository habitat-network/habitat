package pearserver_test

import (
	"github.com/bluesky-social/indigo/atproto/syntax"
)

var (
	org     = syntax.DID("did:plc:org")
	owner   = syntax.DID("did:plc:owner")
	alice   = syntax.DID("did:plc:alice")
	bob     = syntax.DID("did:plc:bob")
	admin   = syntax.DID("did:plc:admin")
	groupTp = syntax.NSID("network.habitat.group")
	docsTp  = syntax.NSID("network.habitat.docs")
)
