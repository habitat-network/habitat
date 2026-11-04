package spacecommit

import (
	"crypto/hmac"
	"errors"
	"fmt"

	"github.com/bluesky-social/indigo/atproto/syntax"

	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// Version is the commit format version, shared with the commit MAC version
// (see [CommitMACType]).
const Version = 1

// ErrInvalidCommit is returned when a commit fails validation: it names the
// wrong repo or space, has an unsupported version, its MAC does not verify, or
// its data does not match the locally recomputed state.
var ErrInvalidCommit = errors.New("invalid commit")

// Commit is the in-memory form of com.atproto.space.defs#commit (and its
// network.habitat.space.defs mirror): a repo's current state digest in one
// space. It carries no signature. The repo host authenticates it to each
// syncer with a MAC in the Atproto-Space-Mac response header, keyed by a
// secret the two negotiate for that one request (see [Decapsulate]), so a
// leaked response proves nothing about authorship to a third party.
type Commit struct {
	Ver   int
	Repo  syntax.DID
	Space habitat_syntax.SpaceURI
	Rev   string
	// Data is the sha256 of the repo's LtHash state (see [LtHash.Sum]).
	Data []byte
}

// New builds a commit over a repo's current state digest.
func New(space habitat_syntax.SpaceURI, repo syntax.DID, rev string, data []byte) Commit {
	return Commit{Ver: Version, Repo: repo, Space: space.Canonical(), Rev: rev, Data: data}
}

// MAC computes the commit's MAC under sharedSecret:
//
//	context = "atproto-space-repo-v1"
//	   || uint16be(len(space)) || space
//	   || uint16be(len(repo))  || repo
//	   || uint16be(len(rev))   || rev
//	mac = HMAC-SHA256(shared_secret, context || data)
func (c Commit) MAC(sharedSecret []byte) []byte {
	b := []byte(CommitMACType + "-v1")
	b = appendVec(b, []byte(c.Space.String()))
	b = appendVec(b, []byte(c.Repo.String()))
	b = appendVec(b, []byte(c.Rev))
	return hmacSHA256(sharedSecret, b, c.Data)
}

// Verify authenticates a commit a syncer received for (space, repo): the
// commit must name that repo and space and carry a supported version, and mac
// must be its MAC under the request's sharedSecret, compared in constant time.
// Only once Verify passes is Data trusted; comparing it with the locally
// folded state is the caller's job. Returns an error wrapping
// [ErrInvalidCommit] on any mismatch.
func (c Commit) Verify(
	space habitat_syntax.SpaceURI,
	repo syntax.DID,
	sharedSecret, mac []byte,
) error {
	if c.Ver != Version {
		return fmt.Errorf("%w: unsupported version %d", ErrInvalidCommit, c.Ver)
	}
	if c.Repo != repo {
		return fmt.Errorf("%w: commit is for repo %s, not %s", ErrInvalidCommit, c.Repo, repo)
	}
	if c.Space.Canonical() != space.Canonical() {
		return fmt.Errorf("%w: commit is for space %s, not %s", ErrInvalidCommit, c.Space, space)
	}
	if !hmac.Equal(c.MAC(sharedSecret), mac) {
		return fmt.Errorf("%w: mac mismatch", ErrInvalidCommit)
	}
	return nil
}

// Matches reports whether the commit's data is the digest of lt. Verify the
// commit first: on its own this says nothing about authenticity.
func (c Commit) Matches(lt *LtHash) bool {
	return hmac.Equal(c.Data, lt.Sum())
}
