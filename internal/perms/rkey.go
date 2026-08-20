package perms

import (
	"crypto/sha256"
	"encoding/base32"
	"strings"

	"github.com/bluesky-social/indigo/atproto/syntax"
	habitat_syntax "github.com/habitat-network/habitat/internal/syntax"
)

// hashRkey derives a deterministic, AT-Proto-record-key-safe string from
// parts by hashing their canonical encoding. Parts are NUL-joined before
// hashing: this is safe only because none of our inputs (DIDs, space URIs,
// role strings) can themselves contain a NUL byte, so no two distinct
// part-sequences can ever collide on the same joined byte string. A leading
// type tag (e.g. "user"/"space") disambiguates the two relation kinds.
func hashRkey(parts ...string) syntax.RecordKey {
	h := sha256.New()
	h.Write([]byte(strings.Join(parts, "\x00")))
	sum := h.Sum(nil)
	// 16 bytes (128 bits) keeps the rkey short while making collisions
	// astronomically unlikely for the number of relations ever stored in a
	// single space.
	return syntax.RecordKey(strings.ToLower(rkeyEncoding.EncodeToString(sum[:16])))
}

// Use base32 because it is a subset of the safe symbols in AT Proto rkeys.
var rkeyEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// userRelationRkey deterministically derives the record key for a
// (did, role) user-relation record from its semantics, so RevokeUserRelation
// can compute the key directly and delete by it instead of scanning every
// record in the collection.
func userRelationRkey(did syntax.DID, role habitat_syntax.SpaceRole) syntax.RecordKey {
	return hashRkey("user", did.String(), string(role))
}

// spaceRelationRkey deterministically derives the record key for a
// (subject, subjectRole, objectRole) space-relation record from its
// semantics, so RevokeSpaceRoleRelation can compute the key directly and
// delete by it instead of scanning every record in the collection.
func spaceRelationRkey(
	subject habitat_syntax.SpaceURI,
	subjectRole habitat_syntax.SpaceRole,
	objectRole habitat_syntax.SpaceRole,
) syntax.RecordKey {
	return hashRkey("space", subject.String(), string(subjectRole), string(objectRole))
}
