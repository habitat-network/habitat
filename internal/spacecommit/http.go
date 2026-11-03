package spacecommit

import (
	"github.com/bluesky-social/indigo/atproto/atdata"
	"github.com/bluesky-social/indigo/atproto/syntax"

	"github.com/habitat-network/habitat/api/comatproto"
	"github.com/habitat-network/habitat/api/habitat"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// ToXRPC shapes a Commit as the network.habitat.space.defs#commit lexicon
// type for JSON responses.
func (c Commit) ToXRPC() habitat.NetworkHabitatSpaceDefsCommit {
	return habitat.NetworkHabitatSpaceDefsCommit{
		Ver:   int64(c.Ver),
		Repo:  c.Repo.String(),
		Space: c.Space.String(),
		Rev:   c.Rev,
		Data:  atdata.Bytes(c.Data),
	}
}

// FromXRPC maps the lexicon JSON form of a commit (bytes fields are
// {"$bytes": "<base64>"} objects per the atproto lexicon spec, decoded by
// habitat.NetworkHabitatSpaceDefsCommit's own UnmarshalJSON) to its in-memory
// form. Its repo and space are taken as given; [Commit.Verify] checks them
// against the request.
func FromXRPC(c habitat.NetworkHabitatSpaceDefsCommit) Commit {
	return Commit{
		Ver:   int(c.Ver),
		Repo:  syntax.DID(c.Repo),
		Space: habitat_syntax.SpaceURI(c.Space),
		Rev:   c.Rev,
		Data:  c.Data,
	}
}

// ToComAtprotoXRPC is ToXRPC for the com.atproto.space.defs#commit type.
func (c Commit) ToComAtprotoXRPC() comatproto.ComAtprotoSpaceDefsCommit {
	return comatproto.ComAtprotoSpaceDefsCommit{
		Ver:   int64(c.Ver),
		Repo:  c.Repo.String(),
		Space: c.Space.String(),
		Rev:   c.Rev,
		Data:  atdata.Bytes(c.Data),
	}
}
