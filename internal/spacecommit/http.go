package spacecommit

import (
	"github.com/bluesky-social/indigo/atproto/atdata"

	"github.com/habitat-network/habitat/api/habitat"
)

// ToXRPC shapes a SignedCommit as the lexicon signedCommit type for JSON
// responses.
func (c SignedCommit) ToXRPC() habitat.NetworkHabitatSpaceDefsSignedCommit {
	return habitat.NetworkHabitatSpaceDefsSignedCommit{
		Ver:  int64(c.Ver),
		Hash: atdata.Bytes(c.Hash),
		Ikm:  atdata.Bytes(c.Ikm),
		Mac:  atdata.Bytes(c.Mac),
		Sig:  atdata.Bytes(c.Sig),
		Rev:  c.Rev,
	}
}

// FromXRPC maps the lexicon JSON form of a signed commit (bytes fields are
// {"$bytes": "<base64>"} objects per the atproto lexicon spec, decoded by
// habitat.NetworkHabitatSpaceDefsSignedCommit's own UnmarshalJSON) to its
// in-memory form.
func FromXRPC(c habitat.NetworkHabitatSpaceDefsSignedCommit) SignedCommit {
	return SignedCommit{
		Ver:  int(c.Ver),
		Hash: c.Hash,
		Ikm:  c.Ikm,
		Mac:  c.Mac,
		Sig:  c.Sig,
		Rev:  c.Rev,
	}
}
